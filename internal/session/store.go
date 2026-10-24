package session

import (
	"context"
	"errors"

	"github.com/lisk0vian/assess/internal/exam"
)

// ErrNotFound is returned by stores when a record does not exist.
var ErrNotFound = errors.New("not found")

// Store persists the session domain. Update runs fn in a write transaction
// (serialized with other writes); View runs fn in a read transaction.
type Store interface {
	Update(ctx context.Context, fn func(Tx) error) error
	View(ctx context.Context, fn func(Tx) error) error
}

// Tx is the set of operations available inside a transaction.
type Tx interface {
	PutPublished(p *exam.Published) error // no-op if the hash already exists
	GetPublished(hash string) (*exam.Published, error)

	InsertSession(s *Session) error
	GetSession(id string) (*Session, error)
	GetOpenSessionByCode(code string) (*Session, error) // lobby or running
	ListOpenSessions() ([]*Session, error)
	ListSessions() ([]*Session, error) // all sessions, oldest first
	UpdateSession(s *Session) error

	InsertParticipant(p *Participant) error
	DeleteParticipant(id string) error // only for participants without an attempt
	GetParticipant(id string) (*Participant, error)
	GetParticipantByStudent(sessionID, studentID string) (*Participant, error)
	ListParticipants(sessionID string) ([]*Participant, error)

	InsertAttempt(a *Attempt) error
	GetAttempt(id string) (*Attempt, error)
	GetAttemptByParticipant(participantID string) (*Attempt, error)
	ListAttempts(sessionID string) ([]*Attempt, error)
	UpdateAttempt(a *Attempt) error

	// UpsertAnswer stores a.Response unless a stored answer for the same
	// question already has a sequence number >= a.Seq. It reports whether
	// the answer was applied.
	UpsertAnswer(a *Answer) (bool, error)
	ListAnswers(attemptID string) ([]*Answer, error)
	SetAnswerGrade(attemptID, questionID string, points float64, correct bool) error

	InsertEvent(e *Event) error
	ListEvents(sessionID string) ([]*Event, error)
}
