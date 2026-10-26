package firewall

import (
	"strings"
	"testing"
)

func TestCommands(t *testing.T) {
	c := Commands(Options{Port: 27337})
	want := `netsh advfirewall firewall add rule name="Assess (ax)" dir=in action=allow protocol=TCP localport=27337-27366 profile=domain,private`
	if !strings.HasPrefix(c[1], want) {
		t.Fatalf("add = %s", c[1])
	}
	if !strings.Contains(Commands(Options{Port: 8000, Public: true})[1], "localport=8000-8029 profile=domain,private,public") {
		t.Fatal("public and another port")
	}
	s := Script(Options{Port: 27337}, "line one\nline two")
	if !strings.Contains(s, "rem line two\r\n") || !strings.Contains(s, c[0]+"\r\n") {
		t.Fatalf("script:\n%s", s)
	}
}

const exe = `D:\usb\ax.exe`

func TestReady(t *testing.T) {
	s := &Status{Supported: true, Port: 27337, Networks: []Network{{Interface: "Wi-Fi", Profile: Private}}}
	s.add(ParseRule(`v2.30|Action=Allow|Active=TRUE|Dir=In|Protocol=6|Profile=Domain|Profile=Private|LPort=27337-27366|Name=Assess (ax)|`), exe)
	if !s.Rule || !s.Ready() {
		t.Fatal("a private network with the port rule is ready")
	}
	s.Networks = append(s.Networks, Network{Interface: "Ethernet", Profile: Public})
	if s.Ready() {
		t.Fatal("a public network is not covered")
	}
	if !(&Status{Supported: false}).Ready() {
		t.Fatal("no firewall to manage")
	}
}

func TestRulesFromRegistry(t *testing.T) {
	pub := []Network{{Interface: "Wi-Fi", Profile: Public}}
	cases := map[string]struct {
		rules []string
		ready bool
	}{
		"this copy, allowed by Windows": {[]string{`v2.10|Action=Allow|Active=TRUE|Dir=In|Protocol=6|Profile=Private|Profile=Public|App=d:\USB\ax.exe|Name=ax.exe|`}, true},
		"another copy":                  {[]string{`v2.10|Action=Allow|Active=TRUE|Dir=In|Protocol=6|Profile=Public|App=C:\temp\ax.exe|Name=ax.exe|`}, false},
		"blocked after a cancelled prompt": {[]string{
			`v2.30|Action=Allow|Active=TRUE|Dir=In|Protocol=6|LPort=27337-27366|Name=Assess (ax)|`,
			`v2.10|Action=Block|Active=TRUE|Dir=In|Protocol=6|Profile=Public|App=D:\usb\ax.exe|Name=ax.exe|`}, false},
		"too short a range": {[]string{`v2.30|Action=Allow|Active=TRUE|Dir=In|Protocol=6|LPort=27337|Name=Assess (ax)|`}, false},
		"disabled":          {[]string{`v2.30|Action=Allow|Active=FALSE|Dir=In|Protocol=6|LPort=27337-27366|Name=Assess (ax)|`}, false},
		"UDP only":          {[]string{`v2.30|Action=Allow|Active=TRUE|Dir=In|Protocol=17|LPort=27337-27366|Name=Assess (ax)|`}, false},
		"an IT port rule":   {[]string{`v2.30|Action=Allow|Active=TRUE|Dir=In|Protocol=6|LPort=27000-28000|Name=School apps|`}, true},
	}
	for name, c := range cases {
		s := &Status{Supported: true, Port: 27337, Networks: pub}
		for _, r := range c.rules {
			s.add(ParseRule(r), exe)
		}
		if s.Ready() != c.ready {
			t.Errorf("%s: ready = %v", name, s.Ready())
		}
	}
}
