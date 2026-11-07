package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Student is a person known to the vault, across sessions. Ref is the
// identifier they type when joining (participants.student_id); names may
// repeat, identifiers do not.
type Student struct {
	ID        string
	Ref       string
	Name      string
	Note      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Class is a named list of students ("3B").
type Class struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// ErrConflict is returned when a change would make two records collide.
var ErrConflict = errors.New("conflict")

// PeopleTx is the storage of students and classes.
type PeopleTx interface {
	// EnsureStudent inserts st unless a student with st.Ref exists, which
	// is then returned unchanged.
	EnsureStudent(st *Student) (*Student, error)
	UpdateStudent(st *Student) error // by ID: ref, name, note
	GetStudent(id string) (*Student, error)
	GetStudentByRef(ref string) (*Student, error)
	ListStudents() ([]*Student, error)
	DeleteStudent(id string) error

	InsertClass(c *Class) error
	UpdateClass(c *Class) error
	GetClass(id string) (*Class, error)
	GetClassByName(name string) (*Class, error) // case-insensitive
	ListClasses() ([]*Class, error)
	DeleteClass(id string) error // members are removed, students stay

	AddMember(classID, studentID string) (bool, error) // false when already in
	RemoveMember(classID, studentID string) error
	ListMembers(classID string) ([]string, error) // student ids
	Memberships() (map[string][]string, error)    // student id → class ids

	// ParticipationsOf lists every participant record with that identifier.
	ParticipationsOf(ref string) ([]*Participant, error)
	// SetParticipantRef changes an identifier in every session.
	SetParticipantRef(oldRef, newRef string) error
	// ForgetParticipant deletes a participant and everything recorded about
	// their attempt.
	ForgetParticipant(participantID string) error
}

// NormalizeName collapses spaces.
func NormalizeName(name string) string { return strings.Join(strings.Fields(name), " ") }

func checkStudent(ref, name string) (string, string, error) {
	return checkIdentity(ref, name)
}

// CheckClassName validates a class name.
func CheckClassName(name string) (string, error) {
	name = NormalizeName(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return "", fmt.Errorf("%w: a class name needs 1 to 40 characters", ErrInvalidInput)
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return "", fmt.Errorf("%w: a class name cannot contain / \\ : * ? \" < > |", ErrInvalidInput)
	}
	return name, nil
}

// People is the vault's directory of students and classes.
type People struct {
	List    []*Student
	Classes []*Class
	ByID    map[string]*Student
	ByRef   map[string]*Student
	Members map[string][]string // class id → student ids, by name
	Of      map[string][]string // student id → class ids
}

// ClassNames returns the names of the classes of a student.
func (p *People) ClassNames(studentID string) []string {
	var out []string
	for _, cid := range p.Of[studentID] {
		for _, c := range p.Classes {
			if c.ID == cid {
				out = append(out, c.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Class finds a class by name, case-insensitively.
func (p *People) Class(name string) *Class {
	for _, c := range p.Classes {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

var nameOrder = collate.New(language.Spanish, collate.IgnoreCase, collate.IgnoreDiacritics)

// SortStudents orders by name, then identifier, the way people expect
// (accents and case do not count).
func SortStudents(list []*Student) {
	sort.SliceStable(list, func(i, j int) bool {
		if c := nameOrder.CompareString(list[i].Name, list[j].Name); c != 0 {
			return c < 0
		}
		return list[i].Ref < list[j].Ref
	})
}

// People reads every student and class.
func (s *Service) People(ctx context.Context) (*People, error) {
	p := &People{ByID: map[string]*Student{}, ByRef: map[string]*Student{}, Members: map[string][]string{}}
	err := s.store.View(ctx, func(tx Tx) error {
		var err error
		if p.List, err = tx.ListStudents(); err != nil {
			return err
		}
		if p.Classes, err = tx.ListClasses(); err != nil {
			return err
		}
		p.Of, err = tx.Memberships()
		return err
	})
	if err != nil {
		return nil, err
	}
	SortStudents(p.List)
	for _, st := range p.List {
		p.ByID[st.ID], p.ByRef[st.Ref] = st, st
	}
	sort.Slice(p.Classes, func(i, j int) bool { return nameOrder.CompareString(p.Classes[i].Name, p.Classes[j].Name) < 0 })
	for _, st := range p.List { // members in name order
		for _, cid := range p.Of[st.ID] {
			p.Members[cid] = append(p.Members[cid], st.ID)
		}
	}
	return p, nil
}

// AddStudent creates a student; ErrDuplicate when the identifier exists.
func (s *Service) AddStudent(ctx context.Context, ref, name, note string) (*Student, error) {
	ref, name, err := checkStudent(ref, name)
	if err != nil {
		return nil, err
	}
	var st *Student
	err = s.store.Update(ctx, func(tx Tx) error {
		if _, err := tx.GetStudentByRef(ref); err == nil {
			return fmt.Errorf("%w: %s", ErrDuplicate, ref)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		now := s.clock()
		st, err = tx.EnsureStudent(&Student{ID: newID(), Ref: ref, Name: name, Note: note, CreatedAt: now, UpdatedAt: now})
		return err
	})
	return st, err
}

// EditStudent changes a student's name and note.
func (s *Service) EditStudent(ctx context.Context, id, name, note string) (*Student, error) {
	name = NormalizeName(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return nil, fmt.Errorf("%w: name must have 1 to 100 characters", ErrInvalidInput)
	}
	var st *Student
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if st, err = tx.GetStudent(id); err != nil {
			return err
		}
		st.Name, st.Note, st.UpdatedAt = name, note, s.clock()
		return tx.UpdateStudent(st)
	})
	return st, err
}

// RenameStudent changes a student's identifier everywhere: the directory
// and every session they took part in.
func (s *Service) RenameStudent(ctx context.Context, id, newRef string) (*Student, error) {
	newRef = NormalizeStudentID(newRef)
	if newRef == "" || utf8.RuneCountInString(newRef) > 64 {
		return nil, fmt.Errorf("%w: student id must have 1 to 64 characters", ErrInvalidInput)
	}
	var st *Student
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if st, err = tx.GetStudent(id); err != nil {
			return err
		}
		if st.Ref == newRef {
			return nil
		}
		if _, err := tx.GetStudentByRef(newRef); err == nil {
			return fmt.Errorf("%w: %s already exists; use merge", ErrConflict, newRef)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := tx.SetParticipantRef(st.Ref, newRef); err != nil {
			return err
		}
		st.Ref, st.UpdatedAt = newRef, s.clock()
		return tx.UpdateStudent(st)
	})
	return st, err
}

// MergeStudents folds from into into: from's sessions and classes move to
// into, and from is deleted. Two records in the same session conflict.
func (s *Service) MergeStudents(ctx context.Context, fromID, intoID string) error {
	if fromID == intoID {
		return fmt.Errorf("%w: a student cannot be merged with themselves", ErrInvalidInput)
	}
	return s.store.Update(ctx, func(tx Tx) error {
		from, err := tx.GetStudent(fromID)
		if err != nil {
			return err
		}
		into, err := tx.GetStudent(intoID)
		if err != nil {
			return err
		}
		a, err := tx.ParticipationsOf(from.Ref)
		if err != nil {
			return err
		}
		b, err := tx.ParticipationsOf(into.Ref)
		if err != nil {
			return err
		}
		inB := map[string]bool{}
		for _, p := range b {
			inB[p.SessionID] = true
		}
		for _, p := range a {
			if inB[p.SessionID] {
				return fmt.Errorf("%w: %s and %s both took part in the same session", ErrConflict, from.Ref, into.Ref)
			}
		}
		if err := tx.SetParticipantRef(from.Ref, into.Ref); err != nil {
			return err
		}
		of, err := tx.Memberships()
		if err != nil {
			return err
		}
		for _, cid := range of[from.ID] {
			if _, err := tx.AddMember(cid, into.ID); err != nil {
				return err
			}
		}
		if into.Note == "" {
			into.Note = from.Note
		}
		into.UpdatedAt = s.clock()
		if err := tx.UpdateStudent(into); err != nil {
			return err
		}
		return tx.DeleteStudent(from.ID)
	})
}

// Footprint is what forgetting a student deletes.
type Footprint struct {
	Student  *Student
	Classes  []string
	Sessions []string // session ids
}

// StudentFootprint lists what ForgetStudent would delete.
func (s *Service) StudentFootprint(ctx context.Context, id string) (*Footprint, error) {
	f := &Footprint{}
	err := s.store.View(ctx, func(tx Tx) error {
		var err error
		if f.Student, err = tx.GetStudent(id); err != nil {
			return err
		}
		parts, err := tx.ParticipationsOf(f.Student.Ref)
		if err != nil {
			return err
		}
		for _, p := range parts {
			f.Sessions = append(f.Sessions, p.SessionID)
		}
		of, err := tx.Memberships()
		if err != nil {
			return err
		}
		for _, cid := range of[id] {
			c, err := tx.GetClass(cid)
			if err != nil {
				return err
			}
			f.Classes = append(f.Classes, c.Name)
		}
		return nil
	})
	sort.Strings(f.Classes)
	return f, err
}

// ErrInUse is returned when forgetting a student of a session still open.
var ErrInUse = errors.New("in use")

// ForgetStudent deletes a student and everything recorded about them in
// every session: answers, events, penalties. It refuses while one of
// those sessions is still open. It returns the affected sessions.
func (s *Service) ForgetStudent(ctx context.Context, id string) ([]string, error) {
	var touched []string
	err := s.store.Update(ctx, func(tx Tx) error {
		st, err := tx.GetStudent(id)
		if err != nil {
			return err
		}
		parts, err := tx.ParticipationsOf(st.Ref)
		if err != nil {
			return err
		}
		for _, p := range parts {
			sess, err := tx.GetSession(p.SessionID)
			if err != nil {
				return err
			}
			if sess.State != StateEnded {
				return fmt.Errorf("%w: %s is in the open session %s; end it first", ErrInUse, st.Ref, sess.Code)
			}
		}
		for _, p := range parts {
			if err := tx.ForgetParticipant(p.ID); err != nil {
				return err
			}
			touched = append(touched, p.SessionID)
		}
		return tx.DeleteStudent(id)
	})
	return touched, err
}

// CreateClass makes an empty class.
func (s *Service) CreateClass(ctx context.Context, name string) (*Class, error) {
	name, err := CheckClassName(name)
	if err != nil {
		return nil, err
	}
	c := &Class{ID: newID(), Name: name, CreatedAt: s.clock()}
	err = s.store.Update(ctx, func(tx Tx) error {
		if _, err := tx.GetClassByName(name); err == nil {
			return fmt.Errorf("%w: class %s", ErrDuplicate, name)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		return tx.InsertClass(c)
	})
	return c, err
}

// RenameClass renames a class.
func (s *Service) RenameClass(ctx context.Context, id, name string) error {
	name, err := CheckClassName(name)
	if err != nil {
		return err
	}
	return s.store.Update(ctx, func(tx Tx) error {
		c, err := tx.GetClass(id)
		if err != nil {
			return err
		}
		if other, err := tx.GetClassByName(name); err == nil && other.ID != id {
			return fmt.Errorf("%w: class %s", ErrDuplicate, name)
		}
		c.Name = name
		return tx.UpdateClass(c)
	})
}

// DeleteClass removes a class; its students stay in the vault.
func (s *Service) DeleteClass(ctx context.Context, id string) error {
	return s.store.Update(ctx, func(tx Tx) error { return tx.DeleteClass(id) })
}

// Enrollee is one row of a class list.
type Enrollee struct {
	Ref  string
	Name string
}

// EnrollResult reports what Enroll did.
type EnrollResult struct {
	Created  int      // new students
	Added    int      // put in the class
	Already  int      // were in the class
	Renamed  []string // identifiers whose stored name differs (kept as stored)
	Rejected []string // invalid rows
}

// Enroll puts people in a class, creating the students that are new. A
// known identifier keeps its stored name; differences are reported.
func (s *Service) Enroll(ctx context.Context, classID string, people []Enrollee) (*EnrollResult, error) {
	res := &EnrollResult{}
	err := s.store.Update(ctx, func(tx Tx) error {
		if _, err := tx.GetClass(classID); err != nil {
			return err
		}
		now := s.clock()
		for _, e := range people {
			ref, name, err := checkStudent(e.Ref, e.Name)
			if err != nil {
				res.Rejected = append(res.Rejected, fmt.Sprintf("%s %s", e.Ref, e.Name))
				continue
			}
			st, err := tx.GetStudentByRef(ref)
			switch {
			case errors.Is(err, ErrNotFound):
				if st, err = tx.EnsureStudent(&Student{ID: newID(), Ref: ref, Name: name, CreatedAt: now, UpdatedAt: now}); err != nil {
					return err
				}
				res.Created++
			case err != nil:
				return err
			case st.Name != name:
				res.Renamed = append(res.Renamed, ref)
			}
			added, err := tx.AddMember(classID, st.ID)
			if err != nil {
				return err
			}
			if added {
				res.Added++
			} else {
				res.Already++
			}
		}
		return nil
	})
	return res, err
}

// Unenroll takes students out of a class.
func (s *Service) Unenroll(ctx context.Context, classID string, studentIDs ...string) error {
	return s.store.Update(ctx, func(tx Tx) error {
		for _, id := range studentIDs {
			if err := tx.RemoveMember(classID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddToSessionClass enrolls a student who joined in the class of the
// session (the "add to the list" of an approval).
func (s *Service) AddToSessionClass(ctx context.Context, sessionID, ref, name string) error {
	ref, name, err := checkStudent(ref, name)
	if err != nil {
		return err
	}
	return s.store.Update(ctx, func(tx Tx) error {
		sess, err := tx.GetSession(sessionID)
		if err != nil {
			return err
		}
		if sess.ClassID == "" {
			return fmt.Errorf("%w: this session has no class", ErrInvalidInput)
		}
		now := s.clock()
		st, err := tx.EnsureStudent(&Student{ID: newID(), Ref: ref, Name: name, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		_, err = tx.AddMember(sess.ClassID, st.ID)
		return err
	})
}

// Participation is one session a student took part in.
type Participation struct {
	Session     *Session
	ExamID      string
	ExamTitle   string
	ClassName   string
	Participant *Participant
	Attempt     *Attempt // nil when they never started
	Strikes     int
	Signals     int
}

// History lists the sessions of a student, oldest first.
func (s *Service) History(ctx context.Context, ref string) ([]*Participation, error) {
	var out []*Participation
	err := s.store.View(ctx, func(tx Tx) error {
		parts, err := tx.ParticipationsOf(ref)
		if err != nil {
			return err
		}
		for _, p := range parts {
			sess, err := tx.GetSession(p.SessionID)
			if err != nil {
				return err
			}
			h := &Participation{Session: sess, Participant: p}
			if h.ExamID, h.ExamTitle, err = tx.PublishedTitle(sess.ExamHash); err != nil {
				return err
			}
			if sess.ClassID != "" {
				if c, err := tx.GetClass(sess.ClassID); err == nil {
					h.ClassName = c.Name
				}
			}
			if a, err := tx.GetAttemptByParticipant(p.ID); err == nil {
				h.Attempt = a
				pens, err := tx.ListPenalties(a.ID)
				if err != nil {
					return err
				}
				for _, pen := range pens {
					if pen.Kind == PenaltyStrike && pen.RevertedAt.IsZero() {
						h.Strikes++
					}
				}
				counts, err := tx.SignalCounts(sess.ID)
				if err != nil {
					return err
				}
				h.Signals = counts[a.ID]
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
			out = append(out, h)
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Session.CreatedAt.Before(out[j].Session.CreatedAt) })
	return out, err
}

// Overview is a session with the names needed to list it.
type Overview struct {
	*Session
	ExamID    string
	ExamTitle string
	ClassName string
	Joined    int
	Submitted int
	Average   float64 // mean percentage of the submitted attempts
}

// Sessions lists every session with its exam and class, oldest first.
func (s *Service) Sessions(ctx context.Context) ([]*Overview, error) {
	var out []*Overview
	err := s.store.View(ctx, func(tx Tx) error {
		all, err := tx.ListSessions()
		if err != nil {
			return err
		}
		classes := map[string]string{}
		if cs, err := tx.ListClasses(); err == nil {
			for _, c := range cs {
				classes[c.ID] = c.Name
			}
		}
		for _, sess := range all {
			info := &Overview{Session: sess, ClassName: classes[sess.ClassID]}
			if info.ExamID, info.ExamTitle, err = tx.PublishedTitle(sess.ExamHash); err != nil {
				return err
			}
			atts, err := tx.ListAttempts(sess.ID)
			if err != nil {
				return err
			}
			var sum float64
			for _, a := range atts {
				info.Joined++
				if !a.SubmittedAt.IsZero() && a.State == AttemptSubmitted && a.MaxScore > 0 {
					info.Submitted++
					sum += a.Score / a.MaxScore * 100
				}
			}
			if info.Submitted > 0 {
				info.Average = sum / float64(info.Submitted)
			}
			out = append(out, info)
		}
		return nil
	})
	return out, err
}

// registerClass pre-registers the members of the session's class.
func registerClass(tx Tx, sess *Session, now time.Time) error {
	if sess.ClassID == "" {
		return nil
	}
	if _, err := tx.GetClass(sess.ClassID); err != nil {
		return err
	}
	ids, err := tx.ListMembers(sess.ClassID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		st, err := tx.GetStudent(id)
		if err != nil {
			return err
		}
		p := &Participant{ID: newID(), SessionID: sess.ID, StudentID: st.Ref, Name: st.Name, Source: SourceRoster, CreatedAt: now}
		if err := tx.InsertParticipant(p); err != nil {
			return err
		}
	}
	return nil
}

// Listed reports whether the student was registered before joining (a
// class member or added by the teacher).
func (s *Service) Listed(ctx context.Context, sessionID, studentID string) (bool, error) {
	var listed bool
	err := s.store.View(ctx, func(tx Tx) error {
		p, err := tx.GetParticipantByStudent(sessionID, NormalizeStudentID(studentID))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		listed = p.Source != SourceSelf
		return nil
	})
	return listed, err
}

// ensureStudent makes everyone who takes part a student of the vault, so
// their history follows their identifier.
func ensureStudent(tx Tx, p *Participant) (*Student, error) {
	return tx.EnsureStudent(&Student{ID: newID(), Ref: p.StudentID, Name: p.Name, CreatedAt: p.CreatedAt, UpdatedAt: p.CreatedAt})
}

// KnownName is the name a student is listed with in a session, empty when
// their identifier is not on it: on the list, the identifier is enough.
func (s *Service) KnownName(ctx context.Context, sessionID, studentID string) (string, error) {
	var name string
	err := s.store.View(ctx, func(tx Tx) error {
		p, err := tx.GetParticipantByStudent(sessionID, NormalizeStudentID(studentID))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err == nil {
			name = p.Name
		}
		return err
	})
	return name, err
}

// SetArchive records where the archive of a session was written.
func (s *Service) SetArchive(ctx context.Context, sessionID, folder string) error {
	return s.store.Update(ctx, func(tx Tx) error { return tx.SetSessionArchive(sessionID, folder) })
}
