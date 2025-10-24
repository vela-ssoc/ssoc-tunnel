package tunnel

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

type authResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (ar *authResponse) String() string {
	if err := ar.checkError(); err != nil {
		return err.Error()
	}

	return "agent 认证成功"
}

func (ar *authResponse) checkError() error {
	if ar.Code >= http.StatusOK && ar.Code < http.StatusMultipleChoices {
		return nil
	}

	return fmt.Errorf("agent 认证失败: %s (%d)", ar.Message, ar.Code)
}

func (ar *authResponse) duplicate() bool {
	return ar.Code == http.StatusConflict
}

type authRequest struct {
	MachineID  ciphertext `json:"machine_id"` // 机器码
	Inet       string     `json:"inet"`       // 出口 IP
	PID        int        `json:"pid"`        // 进程 PID
	Workdir    string     `json:"workdir"`    // 工作目录
	Executable string     `json:"executable"` // 执行路径
	Hostname   string     `json:"hostname"`   // 主机名
	Goos       string     `json:"goos"`       // runtime.GOOS
	Goarch     string     `json:"goarch"`     // runtime.GOARCH
	Semver     string     `json:"semver"`     // 节点版本
	Unload     bool       `json:"unload"`     // 是否开启静默模式，仅在新注册节点时有效
	Unstable   bool       `json:"unstable"`   // 不稳定版本
	Customized string     `json:"customized"` // 定制版本
}

type ciphertext string

func (s ciphertext) LogValue() slog.Value {
	return slog.StringValue(s.String())
}

func (s ciphertext) String() string {
	n := len(s)
	if n <= 16 {
		return strings.Repeat("*", n)
	}

	str := string(s)

	return str[:4] + "******" + str[n-4:]
}
