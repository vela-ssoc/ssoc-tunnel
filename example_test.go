package tunnel_test

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"

	tunnel "github.com/vela-ssoc/ssoc-tunnel"
)

const brokerInternalHost = "broker.ssoc.internal"

func TestExample(t *testing.T) {
	ctx := context.Background()
	cfg := tunnel.Config{
		Addresses: []string{"127.0.0.1:8082"}, // broker 地址必须填写。
		Semver:    "1.0.0-alpha",              // 版本号必须填写
		Unload:    false,                      // 是否开启静默模式，仅在新注册节点时有效
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

	select {}
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
