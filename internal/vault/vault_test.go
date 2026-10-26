package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() { ScryptWorkFactor = 10 }

const pw = "correct horse"

func TestPlainVault(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "v")
	v, rec, err := Init(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec != "" || v.Encrypted() || v.Locked() || v.Meta.Name != "v" {
		t.Fatalf("plain vault = %+v, recovery %q", v.Meta, rec)
	}
	if err := v.WriteFile("classes/3B.yaml", []byte("hola")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "classes", "3B.yaml")); err != nil || string(data) != "hola" {
		t.Fatalf("plain file = %q, %v", data, err)
	}
	if _, _, err := Init(dir, "", ""); err == nil {
		t.Fatal("Init must refuse an existing vault")
	}
	k, err := v.DBKey()
	if k != nil || err != nil {
		t.Fatal("a plain vault has no database key")
	}
}

func TestEncryptedVault(t *testing.T) {
	dir := t.TempDir()
	v, rec, err := Init(dir, "Aula", pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec, "AGE-SECRET-KEY-1") {
		t.Fatalf("recovery key = %q", rec)
	}
	if err := v.WriteFile("exams/x.examx", []byte("la clave secreta")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "exams", "x.examx.age"))
	if err != nil || bytes.Contains(raw, []byte("secreta")) {
		t.Fatalf("encrypted file missing or readable: %v", err)
	}
	meta, _ := os.ReadFile(filepath.Join(dir, MetaFile))
	if bytes.Contains(meta, []byte(pw)) || bytes.Contains(meta, []byte(v.Identity())) {
		t.Fatal("vault.yaml leaks the password or the key")
	}
	key1, _ := v.DBKey()

	// A fresh open is locked; the password, the recovery key or the
	// agent's identity open it, and all give the same database key.
	for name, unlock := range map[string]func(*Vault) error{
		"password": func(v *Vault) error { return v.Unlock(pw) },
		"recovery": func(v *Vault) error { return v.UnlockRecovery(rec) },
		"identity": func(x *Vault) error { return x.UnlockWith(v.Identity()) },
	} {
		w, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !w.Locked() {
			t.Fatal("a reopened vault must be locked")
		}
		if _, err := w.ReadFile("exams/x.examx"); !errors.Is(err, ErrLocked) {
			t.Fatalf("reading while locked: %v", err)
		}
		if err := unlock(w); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if data, err := w.ReadFile("exams/x.examx"); err != nil || string(data) != "la clave secreta" {
			t.Fatalf("%s: read = %q, %v", name, data, err)
		}
		if key2, _ := w.DBKey(); !bytes.Equal(key1, key2) {
			t.Fatalf("%s: the database key changed", name)
		}
	}
	w, _ := Open(dir)
	if err := w.Unlock("wrong password"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}

	// Changing the password keeps the data.
	if err := v.SetPassword("another password"); err != nil {
		t.Fatal(err)
	}
	w, _ = Open(dir)
	if w.Unlock(pw) == nil || w.Unlock("another password") != nil {
		t.Fatal("the new password must replace the old one")
	}

	// A copied folder opens anywhere.
	moved := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Rename(dir, moved); err != nil {
		t.Skip("cannot move the folder:", err)
	}
	m, err := Open(moved)
	if err != nil || m.Unlock("another password") != nil {
		t.Fatalf("moved vault: %v", err)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	dir := t.TempDir()
	v, _, err := Init(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"exams/a.examx": "examen A", "classes/3B.yaml": "Ana García", "sessions/2026/x/summary.md": "nota 9"}
	for n, c := range files {
		if err := v.WriteFile(n, []byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.Encrypt("short"); !errors.Is(err, ErrShortPassword) {
		t.Fatalf("short password: %v", err)
	}
	rec, err := v.Encrypt(pw)
	if err != nil || rec == "" {
		t.Fatal(err)
	}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() == MetaFile {
			return err
		}
		if !strings.HasSuffix(p, Ext) {
			t.Errorf("%s was left plain", p)
		}
		data, _ := os.ReadFile(p)
		for _, c := range files {
			if bytes.Contains(data, []byte(c)) {
				t.Errorf("%s holds %q", p, c)
			}
		}
		return nil
	})
	w, _ := Open(dir)
	if err := w.Unlock(pw); err != nil {
		t.Fatal(err)
	}
	if err := w.Decrypt(); err != nil {
		t.Fatal(err)
	}
	w, _ = Open(dir)
	if w.Encrypted() {
		t.Fatal("still encrypted")
	}
	for n, c := range files {
		if data, err := w.ReadFile(n); err != nil || string(data) != c {
			t.Errorf("%s = %q, %v", n, data, err)
		}
	}
	names, _ := w.Files()
	if len(names) != len(files) {
		t.Fatalf("files = %v", names)
	}
}

func TestLocate(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	reg, _ := LoadRegistry(filepath.Join(root, "vaults.yaml"))
	p, src, _ := Locate(Choice{DataDir: data, Registry: reg})
	if src != FromDefault || p != DefaultDir(data) {
		t.Fatalf("default = %s %s", p, src)
	}
	named := filepath.Join(root, "colegio")
	if _, _, err := Init(named, "colegio", ""); err != nil {
		t.Fatal(err)
	}
	reg.Add("colegio", named)
	if p, src, _ = Locate(Choice{Setting: "Colegio", DataDir: data, Registry: reg}); src != FromSetting || !SamePath(p, named) {
		t.Fatalf("setting = %s %s", p, src)
	}
	project := filepath.Join(root, "curso", "tema1")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Init(filepath.Join(root, "curso", LocalDir), "", ""); err != nil {
		t.Fatal(err)
	}
	if p, src, _ = Locate(Choice{Cwd: project, Setting: "colegio", DataDir: data, Registry: reg}); src != FromFolder || filepath.Base(p) != LocalDir {
		t.Fatalf("folder = %s %s", p, src)
	}
	if p, src, _ = Locate(Choice{Flag: named, Cwd: project, DataDir: data}); src != FromFlag || !SamePath(p, named) {
		t.Fatalf("flag = %s %s", p, src)
	}
	if _, _, err := Locate(Choice{Flag: "nope", DataDir: data, Registry: reg}); err == nil {
		t.Fatal("an unknown vault must fail")
	}
	if reg.Add("colegio", filepath.Join(root, "otro")) != "colegio-2" {
		t.Fatal("a taken name gets a number")
	}
}

func TestAgent(t *testing.T) {
	data := t.TempDir()
	in := strings.NewReader("AGE-SECRET-KEY-1TEST\n")
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunAgent(ctx, data, "v1", 2*time.Second, in, &out) }()
	deadline := time.Now().Add(3 * time.Second)
	for AgentKey(ctx, data, "v1") == "" {
		if time.Now().After(deadline) {
			t.Fatal("the agent did not answer")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := AgentKey(ctx, data, "v1"); got != "AGE-SECRET-KEY-1TEST" {
		t.Fatalf("agent key = %q", got)
	}
	if AgentKey(ctx, data, "other") != "" {
		t.Fatal("another vault has no agent")
	}
	if !LockAgent(ctx, data, "v1") {
		t.Fatal("lock failed")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if AgentKey(ctx, data, "v1") != "" {
		t.Fatal("a locked agent must not answer")
	}
}
