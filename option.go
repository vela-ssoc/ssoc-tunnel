package tunnel

import (
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type HTTPServer interface {
	Serve(net.Listener) error
}

type ConnectNotifier interface {
	// Connected 通道首次连接成功回调
	Connected()

	// Disconnected 断开连接时回调函数
	Disconnected(err error)

	// Reconnected 重连成功的回调函数
	Reconnected()

	// Exited 退出连接，不再重连。
	Exited(err error)
}

// Identifier agent 身份唯一标识。
type Identifier interface {
	// MachineID 获取机器码，机器码是 agent 节点的唯一标识。
	//
	// 在实际环境中，业务方的会进行动态扩缩容，他们会用基础镜像克隆出多个实例，这个基础镜像可能
	// 已经运行过 ssoc-agent，机器码和环境已经初始化过了，而 agent 自身并不知道自己是克隆体，
	// 但是 ssoc 服务就会任务节点在重复连接而拒绝上线。
	//
	// 针对上述问题，设计一种机器码冲突避让策略：
	// 生成机器码时一定要根据操作系统环境生成，不能用时间戳、UUID 等随机的参数作为机器码。
	// 例如：使用计算操作系统的 machine-id + hostname + mac + ip 哈希值作为机器码。即便是
	// 镜像克隆出来的机器，它们的 machine-id 一样，但是 hostname mac ip 不太可能一样，因为
	// 扩缩容的机器一般都同处一个局域网，如果 mac ip 一样，这台机器大概率无法正常联网工作的。
	//
	// 虽然有稳定的生成算法，生成机器码生成后要保存在本地磁盘，如果没有指定要 rebuild 可以直接
	// 读取本地缓存的机器码，为什么要保存在本地呢？因为 agent 的 ip 可能是 DHCP，hostname 也
	// 可能被修改，但机器还是那台机器，如果不缓存每次都生成，久而久之会导致服务端留存大量无效节点。
	//
	// 总结大致思路就是：每次上线时如果服务端检测到重复连接，agent 就 rebuild 重新生成机器码，
	// 每次上线时 rebuild 至多会被调用一次，rebuild 后的机器码可能还是原来的机器码，这说明
	// agent 环境没有发生变化。如果 rebuild 后还是重复上线，也不会再次 rebuild，一般说明存
	// 在其它问题。
	MachineID(rebuild bool) string
}

type Optioner interface {
	options() []func(option) option
}

type option struct {
	ident    Identifier
	server   HTTPServer
	notifier ConnectNotifier
	logger   *slog.Logger
	dialer   *websocket.Dialer
}

type OptionBuilder struct {
	opts []func(option) option
}

func NewOption() OptionBuilder {
	return OptionBuilder{}
}

func (ob OptionBuilder) Identifier(v Identifier) OptionBuilder {
	ob.opts = append(ob.opts, func(o option) option {
		o.ident = v
		return o
	})
	return ob
}

func (ob OptionBuilder) Logger(v *slog.Logger) OptionBuilder {
	ob.opts = append(ob.opts, func(o option) option {
		o.logger = v
		return o
	})
	return ob
}

func (ob OptionBuilder) HTTPServer(v HTTPServer) OptionBuilder {
	ob.opts = append(ob.opts, func(o option) option {
		o.server = v
		return o
	})
	return ob
}

func (ob OptionBuilder) Notifier(v ConnectNotifier) OptionBuilder {
	ob.opts = append(ob.opts, func(o option) option {
		o.notifier = v
		return o
	})
	return ob
}

func (ob OptionBuilder) Dialer(v *websocket.Dialer) OptionBuilder {
	ob.opts = append(ob.opts, func(o option) option {
		o.dialer = v
		return o
	})
	return ob
}

func (ob OptionBuilder) options() []func(option) option {
	return ob.opts
}

func optionEval(opts ...Optioner) option {
	var out option
	for _, opt := range opts {
		for _, op := range opt.options() {
			if op != nil {
				out = op(out)
			}
		}
	}

	return out
}

func fallbackOption() Optioner {
	def := func(o option) option {
		if o.server == nil {
			o.server = new(closedServer)
		}
		if o.ident == nil {
			o.ident = NewIdent(".SSOC_MACHINE_ID")
		}
		if o.notifier == nil {
			o.notifier = new(connectNotifier)
		}
		if o.dialer == nil {
			o.dialer = &websocket.Dialer{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
				HandshakeTimeout: 10 * time.Second,
			}
		}
		return o
	}

	return OptionBuilder{
		opts: []func(option) option{def},
	}
}

type closedServer struct{}

func (closedServer) Serve(ln net.Listener) error {
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		_ = conn.Close()
	}
}

type connectNotifier struct{}

func (c connectNotifier) Connected() {}

func (c connectNotifier) Disconnected(err error) {}

func (c connectNotifier) Reconnected() {}

func (c connectNotifier) Exited(err error) {}
