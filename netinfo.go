package main

import (
	"net"
	"os"
	"strings"
)

// ServerAddr 是一个可访问的服务地址。
type ServerAddr struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// serverAddrs 返回本机可访问的地址列表：
// 优先「主机名.local」（mDNS，IP 变了也不变），再列各网卡的局域网 IPv4。
func serverAddrs(port string) []ServerAddr {
	out := []ServerAddr{}

	if host, err := os.Hostname(); err == nil && host != "" {
		h := strings.TrimSuffix(host, ".")
		if !strings.HasSuffix(h, ".local") {
			h += ".local"
		}
		out = append(out, ServerAddr{Label: "主机名（推荐·IP 变了也能用）", URL: "http://" + h + ":" + port})
	}

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			if ip.IsPrivate() { // 192.168.x / 10.x / 172.16-31.x
				out = append(out, ServerAddr{Label: "局域网 IP", URL: "http://" + ip.String() + ":" + port})
			}
		}
	}

	out = append(out, ServerAddr{Label: "本机", URL: "http://localhost:" + port})
	return out
}
