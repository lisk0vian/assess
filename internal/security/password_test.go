package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordLifecycle(t *testing.T) {
	dir := t.TempDir()
	if HasPassword(dir) || !CheckPassword(dir, "anything") {
		t.Fatal("without a password everything passes")
	}
	if err := SetPassword(dir, "abc"); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short password: %v", err)
	}
	if err := SetPassword(dir, "aula 3B ñandú"); err != nil {
		t.Fatal(err)
	}
	if !HasPassword(dir) || !CheckPassword(dir, "aula 3B ñandú") || CheckPassword(dir, "aula 3b ñandú") || CheckPassword(dir, "") {
		t.Fatal("password check is wrong")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "admin.json"))
	if strings.Contains(string(data), "ñandú") || !strings.Contains(string(data), "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected stored form: %s", data)
	}
	if err := ClearPassword(dir); err != nil || HasPassword(dir) {
		t.Fatal("clear failed")
	}
	if verify("garbage", "x") || verify("$argon2id$v=19$m=1,t=1,p=1$!!$!!", "x") {
		t.Fatal("malformed hashes must not verify")
	}
}
