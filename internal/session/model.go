// Package session holds the exam-session domain: sessions, participants,
// attempts, answers and events, and the service that enforces their rules.
// Storage is abstracted by Store; internal/storage/sqlite implements it.
package session

import (
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/grading"
)

// State of a session.
type State string

// Session states. A session moves lobby → running → ended.
const (
	StateLobby   State = "lobby"
	StateRunning State = "running"
	StateEnded   State = "ended"
)

// Session is one run of a published exam.
type Session struct {
	ID        string
	Code      string // short code students type to join
	ExamHash  string
	State     State
	Duration  time.Duration
	Grace     time.Duration // late answers accepted after the deadline
	CreatedAt time.Time
	StartedAt time.Time // zero until started
	EndsAt    time.Time // absolute end for everyone, before per-attempt extra time
	EndedAt   time.Time // zero until ended
	Secret    string    // derives each attempt's telemetry key; never sent to clients
	ClassID   string    // the class taking the exam, if one was chosen
	// RosterOnly lets in only the students of the class; anybody else
	// waits for the teacher's approval.
	RosterOnly bool
	ArchivedAs string // the vault folder of its archive, once written
}

// ParticipantSource records how a participant was added.
type ParticipantSource string

// Participant sources.
const (
	SourceRoster ParticipantSource = "roster"
	SourceSelf   ParticipantSource = "self"
	SourceManual ParticipantSource = "manual"
)

// Participant is a student registered in a session.
type Participant struct {
	ID        string
	SessionID string
	StudentID string // external identifier, e.g. enrollment number
	Name      string
	Source    ParticipantSource
	CreatedAt time.Time
}

// AttemptState of an attempt.
type AttemptState string

// Attempt states.
const (
	AttemptActive    AttemptState = "active"
	AttemptSubmitted AttemptState = "submitted"
	AttemptAnnulled  AttemptState = "annulled"
	AttemptWithdrawn AttemptState = "withdrawn"
)

// SubmitReason explains how an attempt was closed.
type SubmitReason string

// Submit reasons.
const (
	SubmitManual  SubmitReason = "manual"  // the student submitted
	SubmitTimeout SubmitReason = "timeout" // the deadline passed
	SubmitEnded   SubmitReason = "ended"   // the teacher ended the session
)

// Attempt is one participant's exam.
type Attempt struct {
	ID            string
	SessionID     string
	ParticipantID string
	Seed          uint64
	Layout        []exam.Slot // questions and option order this attempt sees
	State         AttemptState
	Extra         time.Duration // per-attempt extra time
	CreatedAt     time.Time
	SubmittedAt   time.Time
	SubmitReason  SubmitReason
	Score         float64 // valid once submitted
	MaxScore      float64
	LastSeq       int64     // highest client sequence number received for answers
	SignalSeq     int64     // highest sequence number of signed telemetry
	PledgedAt     time.Time // when the student accepted the rules (zero if not yet)
	// NoLens is a reading accommodation granted by the teacher: this student
	// reads prompts whole, without the lens.
	NoLens bool
}

// PenaltyKind is an automatic consequence of a signal.
type PenaltyKind string

// Penalty kinds.
const (
	PenaltyStrike PenaltyKind = "strike"
	PenaltyTime   PenaltyKind = "time"   // Amount ms removed from the attempt's time
	PenaltyVoid   PenaltyKind = "void"   // the question scores 0 and closes
	PenaltyPoints PenaltyKind = "points" // Amount points removed from the score
)

// Penalty is one automatic penalty. It stays in the record when reverted.
type Penalty struct {
	ID         int64
	AttemptID  string
	EventID    int64
	Kind       PenaltyKind
	Signal     string
	QuestionID string
	Amount     float64
	CreatedAt  time.Time
	RevertedAt time.Time
	Note       string
}

// Active reports whether the penalty still applies.
func (p *Penalty) Active() bool { return p.RevertedAt.IsZero() }

// Deadline returns when the attempt's time runs out, or zero if the session
// has not started.
func (a *Attempt) Deadline(s *Session) time.Time {
	if s.EndsAt.IsZero() {
		return time.Time{}
	}
	return s.EndsAt.Add(a.Extra)
}

// Answer is the latest response to one question of an attempt.
type Answer struct {
	AttemptID  string
	QuestionID string
	Response   grading.Response
	Seq        int64 // client sequence number of this change
	UpdatedAt  time.Time
	Graded     bool
	Points     float64
	Correct    bool
}

// Severity of an event.
type Severity string

// Event severities.
const (
	SeverityInfo Severity = "info"
	SeverityWarn Severity = "warn"
	SeverityHigh Severity = "high"
)

// Event is something worth recording about a session or attempt: proctoring
// signals, teacher actions, connection changes.
type Event struct {
	ID         int64
	SessionID  string
	AttemptID  string // empty for session-level events
	Kind       string // e.g. "session.started", "attempt.submitted"
	Severity   Severity
	QuestionID string
	Data       map[string]any
	ClientAt   time.Time // as reported by the browser; informational only
	ReceivedAt time.Time // server time, authoritative
}
