package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/lisk0vian/assess/internal/session"
)

// ---- students ----

const studentCols = `id, ref, name, note, created_at, updated_at`

func (t *tx) scanStudent(sc interface{ Scan(...any) error }) (*session.Student, error) {
	var st session.Student
	var note sql.NullString
	var created, updated sql.NullInt64
	if err := sc.Scan(&st.ID, &st.Ref, &st.Name, &note, &created, &updated); err != nil {
		return nil, err
	}
	var err error
	if st.Ref, err = t.openText(st.Ref); err != nil {
		return nil, err
	}
	if st.Name, err = t.openText(st.Name); err != nil {
		return nil, err
	}
	if st.Note, err = t.openNullText(note); err != nil {
		return nil, err
	}
	st.CreatedAt, st.UpdatedAt = fromMS(created), fromMS(updated)
	return &st, nil
}

// studentArgs seals a student's columns in studentCols order after id.
func (t *tx) studentArgs(st *session.Student) (ref, name string, note sql.NullString, err error) {
	if ref, err = t.sealDet(st.Ref); err != nil {
		return
	}
	if name, err = t.sealText(st.Name); err != nil {
		return
	}
	note, err = t.sealNullText(st.Note)
	return
}

func (t *tx) EnsureStudent(st *session.Student) (*session.Student, error) {
	ref, name, note, err := t.studentArgs(st)
	if err != nil {
		return nil, err
	}
	if _, err := t.exec(`INSERT INTO students (`+studentCols+`) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (ref) DO NOTHING`,
		st.ID, ref, name, note, ms(st.CreatedAt), ms(st.UpdatedAt)); err != nil {
		return nil, err
	}
	return t.GetStudentByRef(st.Ref)
}

func (t *tx) UpdateStudent(st *session.Student) error {
	ref, name, note, err := t.studentArgs(st)
	if err != nil {
		return err
	}
	_, err = t.exec(`UPDATE students SET ref = ?, name = ?, note = ?, updated_at = ? WHERE id = ?`,
		ref, name, note, ms(st.UpdatedAt), st.ID)
	return err
}

func (t *tx) GetStudent(id string) (*session.Student, error) {
	st, err := t.scanStudent(t.row(`SELECT `+studentCols+` FROM students WHERE id = ?`, id))
	return st, notFound(err, "student "+id)
}

func (t *tx) GetStudentByRef(ref string) (*session.Student, error) {
	sealed, err := t.sealDet(ref)
	if err != nil {
		return nil, err
	}
	st, err := t.scanStudent(t.row(`SELECT `+studentCols+` FROM students WHERE ref = ?`, sealed))
	return st, notFound(err, "student "+ref)
}

func (t *tx) ListStudents() ([]*session.Student, error) {
	rows, err := t.query(`SELECT ` + studentCols + ` FROM students`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Student
	for rows.Next() {
		st, err := t.scanStudent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (t *tx) DeleteStudent(id string) error {
	if _, err := t.exec(`DELETE FROM class_members WHERE student_id = ?`, id); err != nil {
		return err
	}
	_, err := t.exec(`DELETE FROM students WHERE id = ?`, id)
	return err
}

// ---- classes ----

func scanClass(sc interface{ Scan(...any) error }) (*session.Class, error) {
	var c session.Class
	var created sql.NullInt64
	if err := sc.Scan(&c.ID, &c.Name, &created); err != nil {
		return nil, err
	}
	c.CreatedAt = fromMS(created)
	return &c, nil
}

func (t *tx) InsertClass(c *session.Class) error {
	_, err := t.exec(`INSERT INTO classes (id, name, created_at) VALUES (?, ?, ?)`, c.ID, c.Name, ms(c.CreatedAt))
	return err
}

func (t *tx) UpdateClass(c *session.Class) error {
	_, err := t.exec(`UPDATE classes SET name = ? WHERE id = ?`, c.Name, c.ID)
	return err
}

func (t *tx) GetClass(id string) (*session.Class, error) {
	c, err := scanClass(t.row(`SELECT id, name, created_at FROM classes WHERE id = ?`, id))
	return c, notFound(err, "class "+id)
}

func (t *tx) GetClassByName(name string) (*session.Class, error) {
	c, err := scanClass(t.row(`SELECT id, name, created_at FROM classes WHERE name = ?`, name))
	return c, notFound(err, "class "+name)
}

func (t *tx) ListClasses() ([]*session.Class, error) {
	rows, err := t.query(`SELECT id, name, created_at FROM classes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Class
	for rows.Next() {
		c, err := scanClass(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (t *tx) DeleteClass(id string) error {
	res, err := t.exec(`DELETE FROM classes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("class %s: %w", id, session.ErrNotFound)
	}
	return nil
}

func (t *tx) AddMember(classID, studentID string) (bool, error) {
	res, err := t.exec(`INSERT INTO class_members (class_id, student_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, classID, studentID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (t *tx) RemoveMember(classID, studentID string) error {
	_, err := t.exec(`DELETE FROM class_members WHERE class_id = ? AND student_id = ?`, classID, studentID)
	return err
}

func (t *tx) ListMembers(classID string) ([]string, error) {
	rows, err := t.query(`SELECT student_id FROM class_members WHERE class_id = ?`, classID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (t *tx) Memberships() (map[string][]string, error) {
	rows, err := t.query(`SELECT student_id, class_id FROM class_members`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var s, c string
		if err := rows.Scan(&s, &c); err != nil {
			return nil, err
		}
		out[s] = append(out[s], c)
	}
	return out, rows.Err()
}

// ---- participants across sessions ----

func (t *tx) ParticipationsOf(ref string) ([]*session.Participant, error) {
	sealed, err := t.sealDet(ref)
	if err != nil {
		return nil, err
	}
	return t.participants(`WHERE student_id = ?`, sealed)
}

func (t *tx) SetParticipantRef(oldRef, newRef string) error {
	from, err := t.sealDet(oldRef)
	if err != nil {
		return err
	}
	to, err := t.sealDet(newRef)
	if err != nil {
		return err
	}
	_, err = t.exec(`UPDATE participants SET student_id = ? WHERE student_id = ?`, to, from)
	return err
}

func (t *tx) ForgetParticipant(participantID string) error {
	var attempt string
	err := t.row(`SELECT id FROM attempts WHERE participant_id = ?`, participantID).Scan(&attempt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if attempt != "" {
		for _, q := range []string{
			`DELETE FROM answers WHERE attempt_id = ?`,
			`DELETE FROM question_opens WHERE attempt_id = ?`,
			`DELETE FROM penalties WHERE attempt_id = ?`,
			`DELETE FROM penalties WHERE event_id IN (SELECT id FROM events WHERE attempt_id = ?)`,
			`DELETE FROM events WHERE attempt_id = ?`,
			`DELETE FROM attempts WHERE id = ?`,
		} {
			if _, err := t.exec(q, attempt); err != nil {
				return err
			}
		}
	}
	_, err = t.exec(`DELETE FROM participants WHERE id = ?`, participantID)
	return err
}
