package tunnel

import (
	"errors"
	"net"
	"strings"
)

type Config struct {
	Addresses  []string `json:"addresses"`  // broker 服务端地址。
	Semver     string   `json:"semver"`     // agent 版本号，例如：1.2.3-alpha。
	Unload     bool     `json:"unload"`     // 是否开启静默模式，仅在新注册节点时有效
	Unstable   bool     `json:"unstable"`   // 不稳定版本
	Customized string   `json:"customized"` // 定制版本
}

func (oc *Config) format() error {
	num := len(oc.Addresses)
	uniq := make(map[string]struct{}, num)
	addrs := make([]string, 0, num)

	for _, addr := range oc.Addresses {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, "443")
		}
		if _, ok := uniq[addr]; ok {
			continue
		}

		uniq[addr] = struct{}{}
		addrs = append(addrs, addr)
	}
	oc.Addresses = addrs
	if len(oc.Addresses) == 0 {
		return errors.New("地址不能为空")
	}

	return nil
}
