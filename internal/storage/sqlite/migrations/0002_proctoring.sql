-- Phase 5: integrity signals, reversible penalties and per-question timing.

-- Automatic penalties. Rows are never deleted: the teacher reverts them.
CREATE TABLE penalties (
    id          INTEGER PRIMARY KEY,
    attempt_id  TEXT NOT NULL REFERENCES attempts (id),
    event_id    INTEGER REFERENCES events (id),
    kind        TEXT NOT NULL CHECK (kind IN ('strike', 'time', 'void', 'points')),
    signal      TEXT NOT NULL,
    question_id TEXT,
    amount      REAL NOT NULL DEFAULT 0, -- ms for time, points for points
    created_at  INTEGER NOT NULL,
    reverted_at INTEGER,
    note        TEXT
) STRICT;

CREATE INDEX penalties_attempt ON penalties (attempt_id);

-- When each question was first opened, to enforce per-question time limits.
CREATE TABLE question_opens (
    attempt_id  TEXT NOT NULL REFERENCES attempts (id),
    question_id TEXT NOT NULL,
    opened_at   INTEGER NOT NULL,
    PRIMARY KEY (attempt_id, question_id)
) STRICT;

ALTER TABLE attempts ADD COLUMN pledged_at INTEGER;
ALTER TABLE attempts ADD COLUMN signal_seq INTEGER NOT NULL DEFAULT 0;

-- Per-session secret used to derive each attempt's telemetry key.
ALTER TABLE sessions ADD COLUMN secret TEXT;
