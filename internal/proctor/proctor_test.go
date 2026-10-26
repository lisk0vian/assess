package proctor

import "testing"

func TestResolveDefaultsAndOverrides(t *testing.T) {
	for _, p := range []string{"", "standard", "strict"} {
		s := Resolve(p, "", "", "", 0)
		if s.Fullscreen != "required" || s.Reveal != "hover" || !s.Enabled() {
			t.Fatalf("%q must protect fully: %+v", p, s)
		}
	}
	if std := Resolve("", "", "", "", 0); std.Profile != "standard" || std.StrikeLimit != 3 || std.Devices != "any" ||
		!std.AutoRead || std.ReadSpeed != ReadNormal {
		t.Fatalf("standard defaults: %+v", std)
	}
	if strict := Resolve("strict", "", "", "", 0); strict.StrikeLimit != 2 || strict.Devices != "desktop" {
		t.Fatalf("strict defaults: %+v", strict)
	}
	custom := Resolve("strict", "suggested", "any", "always", 5)
	if custom.Fullscreen != "suggested" || custom.Devices != "any" || custom.Reveal != "always" || custom.StrikeLimit != 5 {
		t.Fatalf("overrides ignored: %+v", custom)
	}
	off := Resolve("off", "required", "desktop", "hover", 3)
	if off.Enabled() || off.Fullscreen != "off" || off.Reveal != "always" || off.Devices != "any" || !off.Decide(Signal{Kind: Untrusted}).Ignore {
		t.Fatalf("off must turn everything off: %+v", off)
	}
}

func TestDecisions(t *testing.T) {
	strict := Resolve("strict", "", "", "", 0)
	std := Resolve("standard", "", "", "", 0)
	strike := Decision{Severity: High, Warn: true, Strike: true}
	warn := Decision{Severity: Warn, Warn: true}
	away := strike
	away.LoseShare = 0.5 // strict
	awayStd := strike
	awayStd.LoseShare = 0.2
	voids := strike
	voids.VoidQ = true
	cases := []struct {
		name string
		s    Settings
		sig  Signal
		want Decision
	}{
		{"a glance away is ignored (standard)", std, Signal{Kind: Hidden, DurationMs: 2500}, Decision{Ignore: true}},
		{"a glance away is ignored (strict)", strict, Signal{Kind: Hidden, DurationMs: 1000}, Decision{Ignore: true}},
		{"2 s away warns (strict)", strict, Signal{Kind: Hidden, DurationMs: 2000}, warn},
		{"4 s away counts (strict)", strict, Signal{Kind: Hidden, DurationMs: 4000}, away},
		{"12 s away counts (standard)", std, Signal{Kind: Hidden, DurationMs: 12000}, awayStd},
		{"8 s away only warns (standard)", std, Signal{Kind: Hidden, DurationMs: 8000}, warn},
		{"25 s away counts (standard)", std, Signal{Kind: Hidden, DurationMs: 25000}, awayStd},
		{"another window 6 s counts (strict)", strict, Signal{Kind: Blur, DurationMs: 6000}, strike},
		{"another window 6 s warns (standard)", std, Signal{Kind: Blur, DurationMs: 6000}, warn},
		{"first copy counts and voids (strict)", strict, Signal{Kind: Copy, Count: 1}, voids},
		{"first copy warns (standard)", std, Signal{Kind: Copy, Count: 1}, warn},
		{"second copy counts and voids (standard)", std, Signal{Kind: Copy, Count: 2}, voids},
		{"a capture is a copy", strict, Signal{Kind: Screenshot, Count: 1}, voids},
		{"first paste warns (standard)", std, Signal{Kind: PasteInput, Count: 1}, warn},
		{"first paste counts (strict)", strict, Signal{Kind: PasteInput, Count: 1}, strike},
		{"automation counts", std, Signal{Kind: Untrusted}, strike},
		{"extensions only warn", strict, Signal{Kind: DOMInjection}, Decision{Severity: Warn}},
		{"fullscreen exit counts after 5 s (standard)", std, Signal{Kind: FullscreenExit, DurationMs: 6000}, strike},
		{"fullscreen exit counts after 3 s (strict)", strict, Signal{Kind: FullscreenExit, DurationMs: 4000}, strike},
		{"fullscreen exit is informational when suggested", Resolve("standard", "suggested", "", "", 0), Signal{Kind: FullscreenExit, DurationMs: 60000}, Decision{Severity: Info}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Decide(tc.sig); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAntiPhotoSwitchesOneByOne(t *testing.T) {
	no, yes := false, true
	std := Resolve("standard", "", "", "", 0)
	if !std.Lens || !std.FrozenPointer {
		t.Fatalf("the photo protections default to on: %+v", std)
	}
	if s := std.AntiPhoto(&no, nil); s.Lens || !s.FrozenPointer {
		t.Fatalf("only the lens off: %+v", s)
	}
	if s := std.AntiPhoto(nil, &no); !s.Lens || s.FrozenPointer {
		t.Fatalf("only the stillness warning off: %+v", s)
	}
	if s := Resolve("off", "", "", "", 0).AntiPhoto(&yes, &yes); s.Lens || s.FrozenPointer {
		t.Fatalf("off keeps them off: %+v", s)
	}
	if s := std.Reading(&no, ReadFast); s.AutoRead || s.ReadSpeed != ReadFast {
		t.Fatalf("reading settings: %+v", s)
	}
	if s := std.Reading(nil, "nonsense"); s.ReadSpeed != ReadNormal || !s.AutoRead {
		t.Fatalf("an unknown speed keeps the default: %+v", s)
	}
	if s := Resolve("off", "", "", "", 0).Reading(&yes, ReadSlow); s.AutoRead || s.ReadSpeed != "" {
		t.Fatalf("off keeps reading settings off: %+v", s)
	}
}

func TestFrozenPointerOnlyWarnsTheTeacher(t *testing.T) {
	no := false
	std := Resolve("standard", "", "", "", 0)
	strict := Resolve("strict", "", "", "", 0)
	still := func(ms int64) Signal { return Signal{Kind: FrozenPointer, DurationMs: ms} }
	if d := std.Decide(still(25000)); d != (Decision{Severity: Warn}) {
		t.Fatalf("25 s still (standard): %+v", d)
	}
	if d := std.Decide(still(15000)); !d.Ignore {
		t.Fatalf("15 s still is reading (standard): %+v", d)
	}
	if d := strict.Decide(still(15000)); d != (Decision{Severity: Warn}) {
		t.Fatalf("15 s still (strict): %+v", d)
	}
	if d := std.AntiPhoto(nil, &no).Decide(still(60000)); !d.Ignore {
		t.Fatalf("switched off: %+v", d)
	}
}
