-- Assess schema v1. Times are Unix milliseconds (UTC).

CREATE TABLE published_exams (
    hash       TEXT PRIMARY KEY,
    exam_id    TEXT NOT NULL,
    title      TEXT NOT NULL,
    content    BLOB NOT NULL, -- JSON: manifest, questions and answer key
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE published_assets (
    exam_hash TEXT NOT NULL REFERENCES published_exams (hash),
    path      TEXT NOT NULL,
    content   BLOB NOT NULL,
    PRIMARY KEY (exam_hash, path)
) STRICT;

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    code        TEXT NOT NULL,
    exam_hash   TEXT NOT NULL REFERENCES published_exams (hash),
    state       TEXT NOT NULL CHECK (state IN ('lobby', 'running', 'ended')),
    duration_ms INTEGER NOT NULL,
    grace_ms    INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    started_at  INTEGER,
    ends_at     INTEGER,
    ended_at    INTEGER
) STRICT;

-- Codes only need to be unique among sessions students can still join.
CREATE UNIQUE INDEX sessions_open_code ON sessions (code) WHERE state <> 'ended';

CREATE TABLE participants (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions (id),
    student_id TEXT NOT NULL,
    name       TEXT NOT NULL,
    source     TEXT NOT NULL CHECK (source IN ('roster', 'self', 'manual')),
    created_at INTEGER NOT NULL,
    UNIQUE (session_id, student_id)
) STRICT;

CREATE TABLE attempts (
    id             TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL REFERENCES sessions (id),
    participant_id TEXT NOT NULL UNIQUE REFERENCES participants (id),
    seed           INTEGER NOT NULL, -- uint64 stored as its int64 bit pattern
    layout         BLOB NOT NULL,    -- JSON []exam.Slot
    state          TEXT NOT NULL CHECK (state IN ('active', 'submitted', 'annulled', 'withdrawn')),
    extra_ms       INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL,
    submitted_at   INTEGER,
    submit_reason  TEXT,
    score          REAL,
    max_score      REAL NOT NULL,
    last_seq       INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE INDEX attempts_session ON attempts (session_id);

CREATE TABLE answers (
    attempt_id  TEXT NOT NULL REFERENCES attempts (id),
    question_id TEXT NOT NULL,
    response    BLOB NOT NULL, -- JSON grading.Response
    seq         INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    points      REAL,          -- set when the attempt is graded
    correct     INTEGER,
    PRIMARY KEY (attempt_id, question_id)
) STRICT;

CREATE TABLE events (
    id          INTEGER PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions (id),
    attempt_id  TEXT REFERENCES attempts (id),
    kind        TEXT NOT NULL,
    severity    TEXT NOT NULL CHECK (severity IN ('info', 'warn', 'high')),
    question_id TEXT,
    data        BLOB, -- JSON object
    client_at   INTEGER,
    received_at INTEGER NOT NULL
) STRICT;

CREATE INDEX events_session ON events (session_id, received_at);
CREATE INDEX events_attempt ON events (attempt_id, received_at);
