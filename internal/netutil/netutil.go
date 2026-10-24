// Package netutil finds the classroom LAN address and a free port.
package netutil

import (
	"errors"
	"fmt"
	"net"
	"sort"
)

// DefaultPort avoids the ports developers commonly use (3000, 5173, 8000, 8080…).
const DefaultPort = 27337

// LANAddrs returns the private IPv4 addresses of active, non-loopback
// interfaces, most likely classroom address first (192.168 > 10 > 172.16/12).
func LANAddrs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipn.IP.To4(); ip4 != nil && ip4.IsPrivate() {
				out = append(out, ip4)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

func rank(ip net.IP) int {
	switch {
	case ip[0] == 192 && ip[1] == 168:
		return 0
	case ip[0] == 10:
		return 1
	default:
		return 2
	}
}

// Listen binds host:port, or the next free port up to port+tries when
// fallback is set. It returns the listener and the port actually bound.
func Listen(host string, port int, fallback bool) (net.Listener, int, error) {
	tries := 1
	if fallback {
		tries = 30
	}
	var lastErr error
	for p := port; p < port+tries; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(p)))
		if err == nil {
			if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
				p = tcp.Port // port 0 asks the OS for any free port
			}
			return ln, p, nil
		}
		lastErr = err
	}
	return nil, 0, errors.Join(fmt.Errorf("no free port in %d-%d", port, port+tries-1), lastErr)
}
