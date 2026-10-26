-- Phase 9: the vault's students and classes, shared by every session.
-- In an encrypted vault students.ref (deterministically), name and note are
-- sealed, like participants.student_id and name; equal identifiers still
-- seal equally, so participants join students on student_id = ref.

CREATE TABLE students (
    id         TEXT PRIMARY KEY,
    ref        TEXT NOT NULL UNIQUE, -- the identifier students type
    name       TEXT NOT NULL,
    note       TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE classes (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE class_members (
    class_id   TEXT NOT NULL REFERENCES classes (id) ON DELETE CASCADE,
    student_id TEXT NOT NULL REFERENCES students (id) ON DELETE CASCADE,
    PRIMARY KEY (class_id, student_id)
) STRICT;

CREATE INDEX class_members_student ON class_members (student_id);
CREATE INDEX participants_student ON participants (student_id);

-- No foreign key: deleting a class keeps the sessions it sat.
ALTER TABLE sessions ADD COLUMN class_id TEXT;
ALTER TABLE sessions ADD COLUMN roster_only INTEGER NOT NULL DEFAULT 0;
-- The vault folder that holds the session's archive, once written.
ALTER TABLE sessions ADD COLUMN archive TEXT;

-- Everyone who already took part in a session becomes a student.
INSERT INTO students (id, ref, name, created_at, updated_at)
SELECT lower(hex(randomblob(16))), student_id, name, MIN(created_at), MAX(created_at)
FROM participants GROUP BY student_id;
