package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lisk0vian/assess/internal/seal"
)

// sealedColumn is a column whose values are sealed in an encrypted vault.
type sealedColumn struct {
	table, column string
	kind          int
}

const (
	sealBlob = iota
	sealText
	sealDet
)

var sealedColumns = []sealedColumn{
	{"published_exams", "content", sealBlob},
	{"published_assets", "content", sealBlob},
	{"participants", "student_id", sealDet},
	{"participants", "name", sealText},
	{"answers", "response", sealBlob},
	{"events", "data", sealBlob},
	{"penalties", "note", sealText},
	{"students", "ref", sealDet},
	{"students", "name", sealText},
	{"students", "note", sealText},
}

// Reseal rewrites every sealed column with to (nil writes them plain),
// reading them with the database's current cipher, then compacts the file
// so no old value stays behind in free pages or the journal. From then on
// the database uses to.
func (db *DB) Reseal(ctx context.Context, to *seal.Cipher) error {
	t, err := db.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	from := &tx{ctx: ctx, tx: t, c: db.cipher}
	for _, col := range sealedColumns {
		if err := reseal(ctx, t, from, to, col); err != nil {
			t.Rollback()
			return fmt.Errorf("%s.%s: %w", col.table, col.column, err)
		}
	}
	if err := t.Commit(); err != nil {
		return err
	}
	db.cipher, db.locked = to, false
	return db.Compact(ctx)
}

func reseal(ctx context.Context, t *sql.Tx, from *tx, to *seal.Cipher, col sealedColumn) error {
	rows, err := t.QueryContext(ctx, `SELECT rowid, `+col.column+` FROM `+col.table+` WHERE `+col.column+` IS NOT NULL`) //nolint:gosec // G202: fixed table and column names
	if err != nil {
		return err
	}
	type change struct {
		id  int64
		val any
	}
	var changes []change
	for rows.Next() {
		var id int64
		var raw any
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var val any
		switch col.kind {
		case sealBlob:
			b, _ := raw.([]byte)
			plain, err := from.openBlob(b)
			if err != nil {
				rows.Close()
				return err
			}
			val = to.Blob(plain)
		default:
			var s string
			switch v := raw.(type) {
			case string:
				s = v
			case []byte:
				s = string(v)
			}
			plain, err := from.openText(s)
			if err != nil {
				rows.Close()
				return err
			}
			if col.kind == sealDet {
				val = to.Det(plain)
			} else {
				val = to.Text(plain)
			}
		}
		changes = append(changes, change{id, val})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	stmt, err := t.PrepareContext(ctx, `UPDATE `+col.table+` SET `+col.column+` = ? WHERE rowid = ?`) //nolint:gosec // G202: fixed table and column names
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, c := range changes {
		if _, err := stmt.ExecContext(ctx, c.val, c.id); err != nil {
			return err
		}
	}
	return nil
}

// Compact rewrites the file without free pages and empties the journal.
func (db *DB) Compact(ctx context.Context) error {
	if _, err := db.w.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	_, err := db.w.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// BackupTo writes a consistent copy of the database to path while it stays
// in use (VACUUM INTO).
func (db *DB) BackupTo(ctx context.Context, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	_ = os.Remove(path) // VACUUM INTO refuses an existing file
	_, err := db.w.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// Locked reports whether the database was opened without its key.
func (db *DB) Locked() bool { return db.locked }
