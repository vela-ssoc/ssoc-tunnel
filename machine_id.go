package tunnel

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
)

func NewIdent(cachefile string) Identifier {
	return machineIDGenerate{
		cachefile: cachefile,
	}
}

type machineIDGenerate struct {
	cachefile string
}

func (g machineIDGenerate) MachineID(rebuild bool) string {
	if f := g.cachefile; f != "" && !rebuild {
		if raw, _ := os.ReadFile(f); len(raw) != 0 {
			return string(raw)
		}
	}

	return g.generateAndSaveCache()
}

func (g machineIDGenerate) generateAndSaveCache() string {
	mid := g.generate()
	if f := g.cachefile; f != "" {
		_ = os.WriteFile(f, []byte(mid), 0o644)
	}

	return mid
}

func (g machineIDGenerate) generate() string {
	mid, _ := machineID()
	hostname, _ := os.Hostname()
	card := g.networks()
	str := strings.Join([]string{mid, hostname, card}, ",")
	sum := sha1.Sum([]byte(str))

	return hex.EncodeToString(sum[:])
}

func (g machineIDGenerate) networks() string {
	faces, _ := net.Interfaces()
	cards := make(nics, 0, len(faces))
	for _, face := range faces {
		// 跳过换回网卡和未启用的网卡
		if face.Flags&net.FlagUp == 0 ||
			face.Flags&net.FlagLoopback != 0 {
			continue
		}

		var ips []string
		addrs, _ := face.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}

			// 过滤无效地址
			if ip == nil ||
				ip.IsLoopback() ||
				ip.IsMulticast() ||
				ip.IsUnspecified() {
				continue
			}

			if ip4 := ip.To4(); ip4 != nil {
				ips = append(ips, ip.String())
			} else if ip.To16() != nil {
				// 排除 IPv6 链路本地地址（fe80::/10），如不需要可移除此条件
				if ip.IsLinkLocalUnicast() {
					continue
				}
				ips = append(ips, ip.String())
			}
			if len(ips) != 0 {
				cards = append(cards, &nic{
					MAC:   face.HardwareAddr.String(),
					Inets: ips,
				})
			}
		}
	}
	cards.sort()

	return cards.join()
}

type nic struct {
	MAC   string
	Inets []string
}

type nics []*nic

func (ns nics) sort() {
	slices.SortFunc(ns, func(a, b *nic) int {
		return strings.Compare(a.MAC, b.MAC)
	})
	for _, n := range ns {
		slices.Sort(n.Inets)
	}
}

func (ns nics) join() string {
	strs := make([]string, 0, len(ns))
	for _, n := range ns {
		ele := make([]string, 0, len(n.Inets)+1)
		ele = append(ele, n.MAC)
		ele = append(ele, n.Inets...)
		line := strings.Join(ele, ",")
		strs = append(strs, line)
	}

	return strings.Join(strs, ",")
}

// run wraps `exec.Command` with easy access to stdout and stderr.
func run(stdout, stderr io.Writer, cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdin = os.Stdin
	c.Stdout = stdout
	c.Stderr = stderr
	return c.Run()
}

func readFile(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

func trim(s string) string {
	return strings.TrimSpace(strings.Trim(s, "\n"))
}
