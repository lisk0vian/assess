package netutil

import (
	"net"
	"testing"
)

func iface(name, cidr string) Iface {
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return Iface{Name: name, IP: ip, Net: n, Virtual: IsVirtualName(name)}
}

func TestRankAddrs(t *testing.T) {
	got := rankAddrs([]Iface{
		iface("vEthernet (WSL)", "172.20.0.1/20"),
		iface("Ethernet", "83.44.10.20/24"), // a school numbered with public addresses
		iface("Wi-Fi", "192.168.1.30/24"),
		iface("Wi-Fi", "169.254.3.4/16"),
		iface("Wi-Fi", "fe80::1/64"),
	})
	want := []string{"192.168.1.30", "83.44.10.20", "172.20.0.1"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestIsLocal(t *testing.T) {
	_, school, _ := net.ParseCIDR("83.44.10.0/24")
	nets := []*net.IPNet{school}
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "192.168.5.9": true, "10.2.3.4": true, "172.16.0.9": true,
		"100.72.1.1": true, "fe80::2": true, "fd00::5": true,
		"83.44.10.77": true,  // same subnet, public range
		"83.44.11.77": false, // another public network
		"8.8.8.8":     false,
	} {
		if got := IsLocal(net.ParseIP(ip), nets); got != want {
			t.Errorf("IsLocal(%s) = %v, want %v", ip, got, want)
		}
	}
}
