package tunnel

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/xtaci/smux"
)

func Open(ctx context.Context, cfg Config, opts ...Optioner) (Muxer, error) {
	if err := cfg.format(); err != nil {
		return nil, err
	}

	opts = append(opts, fallbackOption())
	opt := optionEval(opts...)

	req := &authRequest{
		PID:        os.Getpid(),
		Goos:       runtime.GOOS,
		Goarch:     runtime.GOARCH,
		Semver:     cfg.Semver,
		Unload:     cfg.Unload,
		Unstable:   cfg.Unstable,
		Customized: cfg.Customized,
	}
	req.Executable, _ = os.Executable()
	req.Workdir, _ = os.Getwd()
	req.Hostname, _ = os.Hostname()

	mux := new(safeMuxer)
	mc := &muxerClient{
		opt: opt,
		cfg: cfg,
		req: req,
		mux: mux,
		ctx: ctx,
	}

	if err := mc.open(); err != nil {
		return nil, err
	}

	ln := &muxerListener{mux: mux}
	go mc.serve(ln)

	return mux, nil
}

type muxerClient struct {
	opt     option
	cfg     Config
	req     *authRequest
	mux     *safeMuxer
	ctx     context.Context
	rebuild bool // 是否已经重新生成过机器码
	remind  bool // 是否已经提醒过机器码重复
}

func (mc *muxerClient) serve(ln net.Listener) {
	const sleep = 3 * time.Second

	mc.opt.notifier.Connected() // 首次连接成功回调函数。

	for {
		srv := mc.opt.server
		err := srv.Serve(ln)
		_ = ln.Close()

		attrs := []any{slog.Any("error", err), slog.Duration("timeout", sleep)}
		mc.log().Warn("agent 掉线了", attrs...)
		mc.opt.notifier.Disconnected(err) // 掉线回调函数。

		_ = mc.sleep(sleep)
		err = mc.open()
		if err != nil {
			mc.opt.notifier.Exited(err) // 退出回调函数。
			break
		}
		mc.opt.notifier.Reconnected() // 重连成功回调函数。
	}
}

func (mc *muxerClient) open() error {
	if mc.req.MachineID == "" {
		first := mc.opt.ident.MachineID(false)
		mc.req.MachineID = ciphertext(first)
	}

	var fails int
	beginAt := time.Now()
	const timeout = 10 * time.Second
	addrs := mc.cfg.Addresses
	mc.log().Info("开始建立通信通道", "addresses", addrs)

	for {
		attrs := []any{
			slog.Time("begin_at", beginAt),
			slog.Any("addresses", addrs),
			slog.Duration("timeout", timeout),
		}
		sess, resp, err := mc.connects(addrs, timeout)
		if err == nil {
			mc.mux.store(sess)
			mc.log().Info("通道连接认证成功", attrs...)
			return nil
		}

		fails++
		sleep := mc.waitN(fails, beginAt)
		attrs = append(attrs, slog.Any("fails", fails), slog.Duration("sleep", sleep), slog.Any("error", err))
		if resp != nil {
			attrs = append(attrs, slog.Any("auth_response", resp))
		}

		mc.log().Warn("通道连接或认证失败", attrs...)
		if err = mc.sleep(sleep); err != nil {
			attrs = append(attrs, slog.Any("sleep_error", err))
			mc.log().Error("context 已取消，agent 隧道不再重连", attrs...)
			return err
		}
	}
}

func (mc *muxerClient) connects(addrs []string, timeout time.Duration) (*smux.Session, *authResponse, error) {
	var errs []error
	for _, addr := range addrs {
		sess, err := mc.dial(addr, timeout) // 通过 websocket 打开底层连接。
		if err != nil {
			errs = append(errs, err)
			continue
		}

		resp, err1 := mc.authentication(sess, timeout)
		if err1 != nil {
			_ = sess.Close()
			continue
		}

		err2 := resp.checkError()
		if err2 == nil {
			return sess, resp, nil
		}

		_ = sess.Close()
		errs = append(errs, err2)
		if !resp.duplicate() {
			continue
		}

		if mc.rebuild {
			if !mc.remind {
				mc.remind = true
				mc.log().Error("重新生成机器码后认证依然提示重复上线，请检查本机是否已经运行了 agent 程序，如果未运行请联系管理员处理。")
			}
			continue
		}

		mc.rebuild = true
		mc.log().Warn("当前机器码已经重复在线，准备 rebuild 机器码")

		before := mc.req.MachineID
		after := ciphertext(mc.opt.ident.MachineID(true))
		mc.req.MachineID = after

		if before == after {
			mc.log().Info("前后机器码生成一致", "machine_id", after)
		} else {
			mc.log().Warn("生成了新的机器码", "before_machine_id", before, "after_machine_id", after)
		}
	}

	return nil, nil, errors.Join(errs...)
}

func (mc *muxerClient) dial(addr string, timeout time.Duration) (*smux.Session, error) {
	ctx, cancel := context.WithTimeout(mc.ctx, timeout)
	defer cancel()

	dialer := mc.opt.dialer
	destURL := &url.URL{Scheme: "ws", Host: addr, Path: "/api/v1/tunnel"}
	strURL := destURL.String()
	ws, _, err := dialer.DialContext(ctx, strURL, nil)
	if err != nil {
		return nil, err
	}

	conn := ws.NetConn()
	sess, err := smux.Client(conn, nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	return sess, nil
}

func (mc *muxerClient) log() *slog.Logger {
	if l := mc.opt.logger; l != nil {
		return l
	}

	return slog.Default()
}

func (mc *muxerClient) sleep(d time.Duration) error {
	ctx := mc.ctx
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func (mc *muxerClient) waitN(fails int, startAt time.Time) time.Duration {
	if fails <= 30 {
		return 2 * time.Second
	} else if fails <= 100 {
		return 5 * time.Second
	} else if fails <= 200 {
		return 10 * time.Second
	}

	if du := time.Since(startAt); du <= 24*time.Hour {
		return 30 * time.Second
	}

	return time.Minute
}

func (mc *muxerClient) authentication(sess *smux.Session, timeout time.Duration) (*authResponse, error) {
	stm, err := sess.OpenStream()
	if err != nil {
		return nil, err
	}
	//goland:noinspection GoUnhandledErrorResult
	defer stm.Close()
	_ = stm.SetDeadline(time.Now().Add(timeout))

	req := mc.req
	switch adt := sess.LocalAddr().(type) {
	case *net.TCPAddr:
		req.Inet = adt.IP.String()
	case *net.UDPAddr:
		req.Inet = adt.IP.String()
	}
	if err = mc.writeRequest(stm, mc.req); err != nil {
		return nil, err
	}

	resp := new(authResponse)
	if err = mc.readResponse(stm, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (mc *muxerClient) writeRequest(stm *smux.Stream, v *authRequest) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	n := len(data)
	if n > 65535 {
		return io.ErrShortWrite
	}
	head := make([]byte, 4)
	binary.BigEndian.PutUint32(head, uint32(n))
	if _, err = stm.Write(head); err != nil {
		return err
	}
	_, err = stm.Write(data)

	return err
}

func (mc *muxerClient) readResponse(stm *smux.Stream, v any) error {
	head := make([]byte, 4)
	n, err := io.ReadFull(stm, head)
	if err != nil {
		return err
	} else if n != 4 {
		return io.ErrShortWrite
	}

	size := binary.BigEndian.Uint32(head)
	data := make([]byte, size)
	if n, err = io.ReadFull(stm, data); err != nil {
		return err
	} else if n != int(size) {
		return io.ErrShortWrite
	}

	return json.Unmarshal(data, v)
}
