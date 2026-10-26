package proctor

// Thresholds are the away limits, in ms: below Warn nothing is recorded,
// above Strike it counts. The browser shows them as countdowns.
type Thresholds struct {
	HiddenWarn       int64 `json:"hidden_warn_ms"`
	HiddenStrike     int64 `json:"hidden_strike_ms"`
	BlurWarn         int64 `json:"blur_warn_ms"`
	BlurStrike       int64 `json:"blur_strike_ms"`
	FullscreenStrike int64 `json:"fullscreen_strike_ms"`
	// Frozen is how long the pointer and keyboard must stay still on an open
	// question before the student's return is reported (frozen_pointer).
	Frozen int64 `json:"frozen_ms"`
}

// Thresholds returns the profile's away limits.
func (s Settings) Thresholds() Thresholds {
	if s.Profile == "strict" {
		return Thresholds{HiddenWarn: 1500, HiddenStrike: 3000, BlurWarn: 2000, BlurStrike: 5000, FullscreenStrike: 3000, Frozen: 12000}
	}
	return Thresholds{HiddenWarn: 3000, HiddenStrike: 10000, BlurWarn: 3000, BlurStrike: 10000, FullscreenStrike: 5000, Frozen: 20000}
}
