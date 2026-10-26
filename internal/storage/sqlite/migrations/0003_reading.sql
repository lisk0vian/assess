-- Reading accommodation: the lens switched off for one student.
ALTER TABLE attempts ADD COLUMN no_lens INTEGER NOT NULL DEFAULT 0;
