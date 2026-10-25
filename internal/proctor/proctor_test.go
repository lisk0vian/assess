package proctor

import (
	"testing"
	"time"
)

func TestResolveDefaultsAndOverrides(t *testing.T) {
	std := Resolve("", "", "", "", 0)
	if std.Profile != "standard" || std.Fullscreen != "suggested" || std.Devices != "any" || std.Reveal != "always" || std.StrikeLimit != 3 {
		t.Fatalf("standard defaults: %+v", std)
	}
	strict := Resolve("strict", "", "", "", 0)
	if strict.Fullscreen != "required" || strict.Devices != "desktop" || strict.Reveal != "hover" {
		t.Fatalf("strict defaults: %+v", strict)
	}
	custom := Resolve("strict", "suggested", "any", "always", 5)
	if custom.Fullscreen != "suggested" || custom.Devices != "any" || custom.Reveal != "always" || custom.StrikeLimit != 5 {
		t.Fatalf("overrides ignored: %+v", custom)
	}
	if off := Resolve("off", "", "", "", 0); off.Enabled() || !off.Decide(Signal{Kind: Untrusted}).Ignore {
		t.Fatal("off must collect nothing")
	}
}

func TestDecisions(t *testing.T) {
	strict := Resolve("strict", "", "", "", 0)
	std := Resolve("standard", "", "", "", 0)
	cases := []struct {
		name string
		s    Settings
		sig  Signal
		want Decision
	}{
		{"short glance away is ignored", strict, Signal{Kind: Hidden, DurationMs: 1500}, Decision{Ignore: true}},
		{"9 s away warns", strict, Signal{Kind: Hidden, DurationMs: 9000}, Decision{Severity: Warn, Warn: true}},
		{"22 s away: strike and a minute (strict)", strict, Signal{Kind: Hidden, DurationMs: 22000}, Decision{Severity: High, Warn: true, Strike: true, LoseTime: time.Minute}},
		{"22 s away only warns (standard)", std, Signal{Kind: Hidden, DurationMs: 22000}, Decision{Severity: Warn, Warn: true}},
		{"40 s away strikes without time (standard)", std, Signal{Kind: Hidden, DurationMs: 40000}, Decision{Severity: High, Warn: true, Strike: true}},
		{"notification blur ignored", strict, Signal{Kind: Blur, DurationMs: 800}, Decision{Ignore: true}},
		{"first copy strikes (strict)", strict, Signal{Kind: Copy, Count: 1}, Decision{Severity: High, Warn: true, Strike: true}},
		{"second copy voids the question (strict)", strict, Signal{Kind: Copy, Count: 2}, Decision{Severity: High, Warn: true, Strike: true, VoidQ: true}},
		{"first copy only warns (standard)", std, Signal{Kind: Copy, Count: 1}, Decision{Severity: Warn, Warn: true}},
		{"automation strikes", std, Signal{Kind: Untrusted}, Decision{Severity: High, Warn: true, Strike: true}},
		{"extensions only warn", strict, Signal{Kind: DOMInjection}, Decision{Severity: Warn}},
		{"fullscreen exit is informational when suggested", std, Signal{Kind: FullscreenExit, DurationMs: 60000}, Decision{Severity: Info}},
		{"fullscreen exit strikes when required", strict, Signal{Kind: FullscreenExit, DurationMs: 12000}, Decision{Severity: High, Warn: true, Strike: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Decide(tc.sig); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
