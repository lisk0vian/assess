package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"

	"github.com/lisk0vian/assess/internal/datadir"
	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/examx"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
	"github.com/lisk0vian/assess/internal/vault"
)

func init() {
	spawnAgents = false // the test binary is not ax
	vault.ScryptWorkFactor = 10
	examx.ScryptWorkFactor = 10
}

// testDB creates the default vault of dir and returns its database file.
func testDB(t *testing.T, dir string) string {
	t.Helper()
	p := vault.DefaultDir(dir)
	if !vault.IsVault(p) {
		if _, _, err := vault.Init(p, "default", ""); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(p, vault.DBFile)
}

// runIn is run with standard input.
func runIn(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// ok runs ax and fails the test on an error.
func ok(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	out, err := runIn(t, stdin, args...)
	if err != nil {
		t.Fatalf("ax %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// treeHas reports whether any file under dir contains text.
func treeHas(t *testing.T, dir, text string) []string {
	t.Helper()
	var hits []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err == nil && bytes.Contains(data, []byte(text)) {
			hits = append(hits, p)
		}
		return nil
	})
	return hits
}

const example = "../../examples/algebra-01"

// sitExam runs a whole session for class on the vault database at dbPath:
// the students in who ("ID Name") join, the exam starts and they submit.
func sitExam(t *testing.T, dbPath, class string, who []string, opts ...sqlite.Option) string {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath, opts...)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := session.NewService(db)
	pub, _, err := exam.Publish(example)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.People(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(ctx, pub, session.CreateOptions{ClassID: p.Class(class).ID})
	if err != nil {
		t.Fatal(err)
	}
	var atts []*session.Attempt
	for _, w := range who {
		id, name, _ := strings.Cut(w, " ")
		a, _, err := svc.Join(ctx, sess.ID, id, name)
		if err != nil {
			t.Fatal(err)
		}
		atts = append(atts, a)
	}
	if _, err := svc.Start(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	// One stays active, so the session is still open for `ax stop`.
	for _, a := range atts[:len(atts)-1] {
		if _, err := svc.Submit(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	return sess.ID
}

func writeRoster(t *testing.T, dir string) string {
	t.Helper()
	text := "Identificador;Nombre;Apellidos\r\nA-12;Ana;García Núñez\r\nB-07;Luis;Pérez\r\nA-12;Ana;García\r\n;Sin;Id\r\n"
	data, err := charmap.Windows1252.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "lista 3B.csv")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlainVaultFlow(t *testing.T) {
	dir := t.TempDir()
	d := []string{"--data-dir", dir}
	csvPath := writeRoster(t, t.TempDir())

	out := ok(t, "", append([]string{"class", "import", "3B", csvPath, "--yes"}, d...)...)
	for _, want := range []string{"Windows-1252", "B-07", "2 ", "A-12 appears more than once", "line 5"} {
		if !strings.Contains(out, want) {
			t.Errorf("import preview lacks %q:\n%s", want, out)
		}
	}
	out = ok(t, "", append([]string{"student", "list", "--class", "3B", "--csv"}, d...)...)
	if !strings.Contains(out, "A-12,Ana García Núñez,3B") {
		t.Fatalf("student list:\n%s", out)
	}
	v, err := vault.Open(vault.DefaultDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := v.ReadFile("classes/3B.yaml"); err != nil || !strings.Contains(string(data), "García Núñez") {
		t.Fatalf("class copy: %q %v", data, err)
	}

	// Exams go into the vault and start from there.
	ok(t, "", append([]string{"vault", "add", example, "--keep"}, d...)...)
	out = ok(t, "", append([]string{"exams", "--csv"}, d...)...)
	if !strings.Contains(out, "algebra-01") {
		t.Fatalf("exams:\n%s", out)
	}
	if _, err := run(t, append([]string{"vault", "add", example, "--keep"}, d...)...); err == nil {
		t.Fatal("adding the same exam twice must need --force")
	}

	// A session of the class, ended without a server, is archived.
	sessID := sitExam(t, v.DB(), "3B", []string{"A-12 Ana García Núñez", "Z-99 Zoe Extra"})
	db, _ := sqlite.Open(context.Background(), v.DB())
	svc := session.NewService(db)
	listed, _ := svc.Listed(context.Background(), sessID, "B-07")
	db.Close()
	if !listed {
		t.Fatal("B-07 must be pre-registered as a class member")
	}
	ok(t, "", append([]string{"stop", "--yes"}, d...)...)
	names, _ := v.List(vault.SessionDir)
	if len(names) != 3 || !strings.HasSuffix(names[0], "record.json") && !strings.HasSuffix(names[1], "record.json") {
		t.Fatalf("archive: %v", names)
	}
	sum := ""
	for _, n := range names {
		if strings.HasSuffix(n, "summary.md") {
			data, _ := v.ReadFile(n)
			sum = string(data)
		}
	}
	if !strings.Contains(sum, "Zoe Extra") || !strings.Contains(sum, "3B") {
		t.Fatalf("summary:\n%s", sum)
	}

	// Finding: by identifier straight away, otherwise the candidates.
	out = ok(t, "", append([]string{"student", "A-12", "--csv"}, d...)...)
	if !strings.Contains(out, "Álgebra") && !strings.Contains(out, "lgebra") {
		t.Fatalf("history of A-12:\n%s", out)
	}
	_, err = run(t, append([]string{"student", "garsia"}, d...)...)
	if err == nil || !strings.Contains(err.Error(), "A-12") {
		t.Fatalf("a near miss must list the candidates: %v", err)
	}
	out = ok(t, "", append([]string{"find", "nunez", "--csv"}, d...)...)
	if !strings.Contains(out, "A-12") {
		t.Fatalf("find:\n%s", out)
	}
	out = ok(t, "", append([]string{"find", "--type", "student", "--below", "101", "--csv"}, d...)...)
	if !strings.Contains(out, "A-12") || strings.Contains(out, "B-07") {
		t.Fatalf("find --below (B-07 was absent):\n%s", out)
	}

	// Gradebook as Excel.
	xlsx := filepath.Join(t.TempDir(), "3B.xlsx")
	ok(t, "", append([]string{"gradebook", "3B", "--out", xlsx}, d...)...)
	if zr, err := zip.OpenReader(xlsx); err != nil || len(zr.File) < 5 {
		t.Fatalf("xlsx: %v", err)
	} else {
		zr.Close()
	}
	out = ok(t, "", append([]string{"gradebook", "3B", "--csv"}, d...)...)
	if !strings.Contains(out, "B-07,Luis Pérez,") {
		t.Fatalf("gradebook csv:\n%s", out)
	}

	// Backup and restore into another folder.
	out = ok(t, "", append([]string{"backup"}, d...)...)
	list, _ := os.ReadDir(filepath.Join(v.Dir, vault.BackupDir))
	var zipFile string
	for _, e := range list {
		if strings.HasPrefix(e.Name(), "backup-") {
			zipFile = filepath.Join(v.Dir, vault.BackupDir, e.Name())
		}
	}
	if zipFile == "" {
		t.Fatalf("no backup written:\n%s", out)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	ok(t, "", append([]string{"restore", zipFile, restored}, d...)...)
	out = ok(t, "", append([]string{"student", "list", "--csv", "--vault", restored}, d...)...)
	if !strings.Contains(out, "Z-99") {
		t.Fatalf("restored vault:\n%s", out)
	}

	// Forgetting removes the student everywhere, archives included.
	ok(t, "", append([]string{"student", "forget", "Z-99", "--yes"}, d...)...)
	if hits := treeHas(t, v.Dir, "Zoe Extra"); len(hits) > 0 {
		// Backups made before keep it, by design.
		for _, h := range hits {
			if !strings.Contains(h, string(filepath.Separator)+vault.BackupDir+string(filepath.Separator)) {
				t.Errorf("%s still mentions the forgotten student", h)
			}
		}
	}
}

func TestEncryptedVaultFlow(t *testing.T) {
	dir := t.TempDir()
	vdir := filepath.Join(t.TempDir(), "aula")
	const pw = "una contraseña"
	oldTerm, oldSecret := isTerminal, readSecret
	isTerminal = func() bool { return true }
	asked := 0
	readSecret = func(string) (string, error) { asked++; return pw, nil }
	t.Cleanup(func() { isTerminal, readSecret = oldTerm, oldSecret; clear(unlocked) })
	d := []string{"--data-dir", dir, "--vault", vdir}

	out := ok(t, "\n", "vault", "init", vdir, "--name", "aula", "--data-dir", dir)
	if !strings.Contains(out, "AGE-SECRET-KEY-1") {
		t.Fatalf("init must show the recovery key:\n%s", out)
	}
	clear(unlocked) // a new command asks for the password
	asked = 0
	ok(t, "", append([]string{"class", "import", "3B", writeRoster(t, t.TempDir()), "--yes"}, d...)...)
	if asked != 1 {
		t.Fatalf("the password was asked %d times", asked)
	}
	ok(t, "", append([]string{"vault", "add", example, "--keep"}, d...)...)

	v, _ := vault.Open(vdir)
	if err := v.Unlock(pw); err != nil {
		t.Fatal(err)
	}
	c, _ := v.Cipher()
	opt := sqlite.WithCipher(c, true)
	sitExam(t, v.DB(), "3B", []string{"A-12 Ana García Núñez", "B-07 Luis Pérez"}, opt)
	ok(t, "y\n", append([]string{"stop", "--yes"}, d...)...)

	db, _ := sqlite.Open(context.Background(), v.DB(), opt)
	_ = db.Compact(context.Background())
	db.Close()
	for _, secret := range []string{"García", "A-12", "Resuelve la ecuación", "answer-key"} {
		if hits := treeHas(t, vdir, secret); len(hits) > 0 {
			t.Errorf("%q is readable in %v", secret, hits)
		}
	}

	// The panel is protected by the same password.
	if !protected(dir) {
		t.Fatal("an encrypted vault protects the panel")
	}

	// Decrypting brings everything back in the clear.
	ok(t, "", append([]string{"vault", "decrypt", "--yes"}, d...)...)
	out = ok(t, "", append([]string{"student", "A-12", "--csv"}, d...)...)
	if !strings.Contains(out, "3B") {
		t.Fatalf("after decrypting:\n%s", out)
	}
	if hits := treeHas(t, vdir, "García Núñez"); len(hits) == 0 {
		t.Fatal("a decrypted vault is readable")
	}
}

func TestLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, datadir.DB(dir))
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _ := exam.Publish(example)
	svc := session.NewService(db)
	sess, _ := svc.CreateSession(ctx, pub, session.CreateOptions{})
	if _, _, err := svc.Join(ctx, sess.ID, "OLD-1", "Olga Antigua"); err != nil {
		t.Fatal(err)
	}
	if err := svc.End(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out := ok(t, "", "student", "OLD-1", "--data-dir", dir)
	if !strings.Contains(out, "Olga Antigua") {
		t.Fatalf("the old student is not in the vault:\n%s", out)
	}
	if _, err := os.Stat(datadir.DB(dir)); !os.IsNotExist(err) {
		t.Fatal("the old database must move into the vault")
	}
	v, _ := vault.Open(vault.DefaultDir(dir))
	backups, _ := os.ReadDir(filepath.Join(v.Dir, vault.BackupDir))
	if len(backups) == 0 || !strings.HasPrefix(backups[0].Name(), "pre-vault-") {
		t.Fatalf("the original is kept as a backup: %v", backups)
	}
	if names, _ := v.List(vault.SessionDir); len(names) != 3 {
		t.Fatalf("the finished session is archived: %v", names)
	}
}
