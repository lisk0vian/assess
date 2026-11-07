// Package netutil finds the addresses students can reach and a free port.
package netutil

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// DefaultPort avoids the ports developers commonly use (3000, 5173, 8000, 8080…).
const DefaultPort = 27337

// PortRange is how many ports from the configured one the server may try
// when it is busy; `ax firewall open` opens exactly this range.
const PortRange = 30

// Iface is one address of a network interface.
type Iface struct {
	Name    string
	IP      net.IP
	Net     *net.IPNet
	Virtual bool // a virtual machine, WSL or container adapter
}

// virtualNames mark adapters students never reach.
var virtualNames = []string{"vethernet", "wsl", "virtualbox", "vbox", "vmware", "vmnet", "docker", "hyper-v", "loopback",
	"tailscale", "zerotier", "vpn", "tap", "tun", "utun", "br-", "virbr", "veth"}

// IsVirtualName reports whether an interface name looks like a virtual adapter.
func IsVirtualName(name string) bool {
	n := strings.ToLower(name)
	for _, v := range virtualNames {
		if strings.Contains(n, v) {
			return true
		}
	}
	return false
}

// Interfaces lists the addresses of the active, non-loopback interfaces.
func Interfaces() []Iface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []Iface
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				out = append(out, Iface{Name: ifc.Name, IP: ipn.IP, Net: ipn, Virtual: IsVirtualName(ifc.Name)})
			}
		}
	}
	return out
}

// LANAddrs returns the IPv4 addresses students can type, the likeliest
// classroom address first: real adapters before virtual ones, then
// 192.168 > 10 > 172.16/12 > any other (schools that number their network
// with public or carrier-grade ranges). Link-local addresses are left out.
func LANAddrs() []net.IP { return rankAddrs(Interfaces()) }

func rankAddrs(ifs []Iface) []net.IP {
	type cand struct {
		ip   net.IP
		rank int
	}
	var cs []cand
	for _, f := range ifs {
		ip4 := f.IP.To4()
		if ip4 == nil || ip4.IsLinkLocalUnicast() || ip4.IsLoopback() {
			continue
		}
		r := rank(ip4)
		if f.Virtual {
			r += 10
		}
		cs = append(cs, cand{ip4, r})
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].rank < cs[j].rank })
	out := make([]net.IP, len(cs))
	for i, c := range cs {
		out[i] = c.ip
	}
	return out
}

func rank(ip net.IP) int {
	switch {
	case ip[0] == 192 && ip[1] == 168:
		return 0
	case ip[0] == 10:
		return 1
	case ip.IsPrivate():
		return 2
	default:
		return 3
	}
}

// LocalNets are the subnets of this computer's interfaces: a device in any
// of them is on the classroom network, whatever its addresses look like.
func LocalNets() []*net.IPNet {
	var out []*net.IPNet
	for _, f := range Interfaces() {
		if f.Net != nil && !f.Virtual {
			out = append(out, f.Net)
		}
	}
	return out
}

// cgnat is 100.64.0.0/10, used inside some school and carrier networks.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}

// IsLocal reports whether ip belongs to a local network: loopback,
// private, link-local, carrier-grade NAT, or one of nets.
func IsLocal(ip net.IP, nets []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip) {
		return true
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Listen binds host:port, or the next free port up to port+PortRange-1 when
// fallback is set. It returns the listener and the port actually bound.
func Listen(host string, port int, fallback bool) (net.Listener, int, error) {
	tries := 1
	if fallback {
		tries = PortRange
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
