// Package exam loads and validates exam folders in the assess/v1 format.
//
// See docs/format.md for the specification.
package exam

import (
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

// FormatV1 is the only supported format identifier.
const FormatV1 = "assess/v1"

// File layout of an exam folder.
const (
	ManifestFile  = "manifest.yaml"
	QuestionsDir  = "questions"
	PrivateDir    = "private"
	AnswerKeyFile = "private/answer-key.yaml"
	AssetsDir     = "assets"
)

// Limits enforced by validation.
const (
	MaxQuestions      = 500
	MaxOptions        = 10
	MinOptions        = 2
	MaxMarkdownBytes  = 64 << 10
	MaxYAMLFileBytes  = 1 << 20
	MaxAssetsBytes    = 20 << 20
	MaxAssetFiles     = 1000
	MaxDuration       = 24 * time.Hour
	MinTimeLimit      = 5 * time.Second
	MaxPoints         = 1000
	MaxIdentifierSize = 64
)

// Question types.
const (
	TypeSingle    = "single"
	TypeMultiple  = "multiple"
	TypeTrueFalse = "truefalse"
	TypeShort     = "short"
)

// Exam is a fully loaded exam folder.
type Exam struct {
	Dir       string
	Manifest  Manifest
	Questions map[string]*Question // by question id
	Key       AnswerKey
	Assets    []string // slash paths relative to Dir, e.g. "assets/diagram.png"
}

// Manifest is manifest.yaml.
type Manifest struct {
	Format           string     `yaml:"format"`
	ID               string     `yaml:"id"`
	Title            string     `yaml:"title"`
	Language         string     `yaml:"language"`
	Duration         Duration   `yaml:"duration"`
	ResultVisibility string     `yaml:"result_visibility"`
	Instructions     string     `yaml:"instructions"`
	Order            Order      `yaml:"order"`
	Proctoring       Proctoring `yaml:"proctoring"`
	Items            []Item     `yaml:"items"`
	Branding         Branding   `yaml:"branding"`
}

// Order controls per-attempt randomization. Nil means the default (true).
type Order struct {
	ShuffleQuestions *bool `yaml:"shuffle_questions"`
	ShuffleOptions   *bool `yaml:"shuffle_options"`
}

// Proctoring selects the anti-cheat profile. Empty fields take the
// profile's defaults (see internal/proctor).
type Proctoring struct {
	Profile     string `yaml:"profile"`      // off | standard | strict
	Fullscreen  string `yaml:"fullscreen"`   // off | suggested | required
	Devices     string `yaml:"devices"`      // any | desktop
	Reveal      string `yaml:"reveal"`       // always | hover, default for questions
	StrikeLimit int    `yaml:"strike_limit"` // strikes that annul the attempt
}

// Item is one entry of manifest.items: either a fixed question or a pool.
type Item struct {
	Question string `yaml:"question"`
	Pool     *Pool  `yaml:"pool"`
}

// Pool draws Pick questions from From for each attempt.
type Pool struct {
	ID   string   `yaml:"id"`
	Pick int      `yaml:"pick"`
	From []string `yaml:"from"`
}

// Branding customizes the student UI.
type Branding struct {
	Accent string `yaml:"accent"`
	Logo   string `yaml:"logo"`
}

// Question is one file in questions/.
type Question struct {
	ID          string   `yaml:"id"`
	Type        string   `yaml:"type"`
	Prompt      string   `yaml:"prompt"`
	TimeLimit   Duration `yaml:"time_limit"`
	Reveal      string   `yaml:"reveal"`
	Options     []Option `yaml:"options"`
	Input       string   `yaml:"input"`
	Placeholder string   `yaml:"placeholder"`

	File string `yaml:"-"` // slash path relative to the exam folder
}

// Option is a choice of a single or multiple question.
type Option struct {
	ID   string `yaml:"id"`
	Text string `yaml:"text"`
	Pin  bool   `yaml:"pin"`
}

// AnswerKey is private/answer-key.yaml.
type AnswerKey struct {
	Format  string            `yaml:"format"`
	Exam    string            `yaml:"exam"`
	Answers map[string]Answer `yaml:"answers"`
}

// Answer is the scoring entry of one question. Which fields apply depends on
// the question type.
type Answer struct {
	Points    float64  `yaml:"points"`
	Option    string   `yaml:"option"`    // single
	Options   []string `yaml:"options"`   // multiple
	Partial   bool     `yaml:"partial"`   // multiple
	Value     *bool    `yaml:"value"`     // truefalse
	Number    *float64 `yaml:"number"`    // short / number
	Tolerance *float64 `yaml:"tolerance"` // short / number
	Accept    []string `yaml:"accept"`    // short / text
	Normalize []string `yaml:"normalize"` // short / text
}

// Defaults applied by accessors when a field is omitted.
const (
	DefaultLanguage         = "es"
	DefaultResultVisibility = "score"
	DefaultProfile          = "standard"
)

// Lang returns the student UI language.
func (m *Manifest) Lang() string { return orDefault(m.Language, DefaultLanguage) }

// Visibility returns what students see after submitting.
func (m *Manifest) Visibility() string {
	return orDefault(m.ResultVisibility, DefaultResultVisibility)
}

// Profile returns the proctoring profile.
func (m *Manifest) Profile() string { return orDefault(m.Proctoring.Profile, DefaultProfile) }

// ShuffleQuestions reports whether question order is randomized.
func (m *Manifest) ShuffleQuestions() bool {
	return m.Order.ShuffleQuestions == nil || *m.Order.ShuffleQuestions
}

// ShuffleOptions reports whether option order is randomized.
func (m *Manifest) ShuffleOptions() bool {
	return m.Order.ShuffleOptions == nil || *m.Order.ShuffleOptions
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Stats summarizes what a single attempt contains.
type Stats struct {
	TotalQuestions      int     // question files in the exam
	QuestionsPerAttempt int     // fixed questions + pool picks
	PointsPerAttempt    float64 // assuming pools award equal points
}

// Stats computes attempt statistics. It assumes the exam is valid.
func (e *Exam) Stats() Stats {
	s := Stats{TotalQuestions: len(e.Questions)}
	for _, it := range e.Manifest.Items {
		switch {
		case it.Pool != nil:
			s.QuestionsPerAttempt += it.Pool.Pick
			if len(it.Pool.From) > 0 {
				s.PointsPerAttempt += float64(it.Pool.Pick) * e.Key.Answers[it.Pool.From[0]].Points
			}
		case it.Question != "":
			s.QuestionsPerAttempt++
			s.PointsPerAttempt += e.Key.Answers[it.Question].Points
		}
	}
	return s
}

// Duration is a time.Duration written in Go syntax ("90s", "45m", "1h30m").
type Duration time.Duration

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if n.Kind != yaml.ScalarNode || n.Decode(&s) != nil {
		return typeError(n.Line, "expected a duration such as 90s, 45m or 1h30m")
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return typeError(n.Line, fmt.Sprintf("invalid duration %q (use e.g. 90s, 45m, 1h30m)", s))
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML writes the duration in Go syntax.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// typeError reports a field-level decoding problem. Returning *yaml.TypeError
// lets the decoder keep going and report the rest of the file.
func typeError(line int, msg string) error {
	return &yaml.TypeError{Errors: []string{fmt.Sprintf("line %d: %s", line, msg)}}
}
