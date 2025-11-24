package tunnel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/vela-ssoc/ssoc-tunnel"
)

const brokerInternalHost = "broker.ssoc.internal"

func TestExample(t *testing.T) {
	// 此文件已被隐写了数据，实际环境请替换成程序自身，如 os.Args[0]。
	// selfExe := os.Args[0]
	selfExe := "example_steganoed_program.png"

	// 读取隐写数据
	hide := new(hideConfig)
	if err := tunnel.ReadManifest(selfExe, hide); err != nil {
		t.Errorf("读取隐写数据出错: %v", err)
		return
	}
	t.Logf("隐写数据读取成功：%s", hide)

	ctx := context.Background()
	cfg := tunnel.Config{
		Addresses:  hide.Addresses, // broker 地址必须填写。
		Semver:     hide.Semver,    // 版本号必须填写
		Unload:     hide.Unload,    // 是否开启静默模式，仅在新注册节点时有效
		Customized: hide.Customized,
		// ... 其它字段不再一一列举
	}

	opt := tunnel.NewOption().
		HTTPServer(myHTTPServer()).
		Identifier(tunnel.NewIdent(""))
	mux, err := tunnel.Open(ctx, cfg, opt)
	if err != nil {
		t.Errorf("节点上线失败: %v", err)
		return
	}

	httpCli := newHTTPClient(mux)
	{
		// 通过内部通道向 broker 发送消息
		reqURL := internalURL("/api/v1/ping")
		resp, err1 := httpCli.Get(reqURL.String())
		if err1 != nil {
			t.Errorf("内部请求出错了: %v", err1)
		} else {
			_ = resp.Body.Close()
			t.Logf("内部请求通了: %d", resp.StatusCode)
		}
	}
	{
		// 向外部服务发送请求
		resp, err1 := httpCli.Get("https://example.com")
		if err1 != nil {
			t.Errorf("外网请求出错了: %v", err1)
		} else {
			_ = resp.Body.Close()
			t.Logf("外网请求通了: %d", resp.StatusCode)
		}
	}

	uploadWriter := newUploadWriter(httpCli)
	handler := slog.NewJSONHandler(uploadWriter, &slog.HandlerOptions{Level: slog.LevelDebug})
	hlog := slog.New(handler)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for at := range ticker.C {
		hlog.Info("现在时间是：" + at.String())
	}
}

func myHTTPServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("PONG"))
	})

	return &http.Server{Handler: mux}
}

func internalURL(path string) *url.URL {
	return &url.URL{
		Scheme: "http",
		Host:   brokerInternalHost,
		Path:   path,
	}
}

func newHTTPClient(mux tunnel.Muxer) *http.Client {
	systemDialer := new(net.Dialer)
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if host, _, _ := net.SplitHostPort(addr); host == brokerInternalHost {
					return mux.OpenConn(ctx)
				}

				return systemDialer.DialContext(ctx, network, addr)
			},
		},
	}
}

type hideConfig struct {
	Addresses  []string `json:"addresses"`
	Semver     string   `json:"semver"`
	Unload     bool     `json:"unload"`
	Unstable   bool     `json:"unstable"`
	Customized string   `json:"customized"`
	// ... 其它字段不再一一列举，需要和隐写端商量好
}

func (hc hideConfig) String() string {
	dat, _ := json.Marshal(hc)
	return string(dat)
}

func newUploadWriter(cli *http.Client) io.Writer {
	return &consoleLog{cli: cli}
}

type consoleLog struct {
	cli *http.Client
}

func (cl *consoleLog) Write(p []byte) (int, error) {
	n := len(p)
	if n == 0 {
		return n, nil
	}
	data := &consoleData{Content: string(p)}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(data); err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reqURL := internalURL("/api/v1/console/write")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), buf)
	if err != nil {
		return n, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := cl.cli.Do(req)
	if err != nil {
		return 0, err
	}
	fmt.Printf("上报结果: %d\n", resp.StatusCode)
	_ = resp.Body.Close()

	return n, nil
}

type consoleData struct {
	Content string `json:"content"`
}
