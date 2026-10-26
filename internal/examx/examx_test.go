package examx

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func init() { ScryptWorkFactor = 10 }

const example = "../../examples/algebra-01"

func TestPackRoundTrip(t *testing.T) {
	data, idx, err := Pack(example, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if idx.ExamID == "" || !idx.HasKey || idx.Files["private/answer-key.yaml"] == "" {
		t.Fatalf("index = %+v", idx)
	}
	c, err := Read(data)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "out")
	probs, err := c.Extract(dir, false)
	if err != nil || probs.HasErrors() {
		t.Fatalf("extract: %v %v", err, probs)
	}
	if _, err := c.Extract(dir, false); err == nil {
		t.Fatal("extracting over a non-empty folder must fail")
	}
}

func TestNoKey(t *testing.T) {
	data, idx, err := Pack(example, Options{NoKey: true})
	if err != nil {
		t.Fatal(err)
	}
	for name := range idx.Files {
		if strings.HasPrefix(name, "private/") {
			t.Fatalf("%s must be left out", name)
		}
	}
	if bytes.Contains(data, []byte("answer-key")) {
		t.Fatal("the package mentions the key file")
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(c))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestHostilePackages(t *testing.T) {
	cases := map[string]map[string]string{
		"traversal": {"../evil.txt": "x", IndexFile: `{"format":1,"files":{"../evil.txt":""}}`},
		"absolute":  {"/etc/x": "x", IndexFile: `{"format":1,"files":{}}`},
		"drive":     {"C:/x": "x", IndexFile: `{"format":1,"files":{}}`},
		"unlisted":  {"manifest.yaml": "x", IndexFile: `{"format":1,"files":{}}`},
		"tampered":  {"manifest.yaml": "x", IndexFile: `{"format":1,"files":{"manifest.yaml":"00"}}`},
		"no index":  {"manifest.yaml": "x"},
		"future":    {IndexFile: `{"format":99,"files":{}}`},
	}
	for name, files := range cases {
		if _, err := Read(zipOf(t, files)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Read([]byte("not a zip")); !errors.Is(err, ErrNotPackage) {
		t.Errorf("garbage: %v", err)
	}
}

func TestPassphrase(t *testing.T) {
	data, _, err := Pack(example, Options{})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := Seal(data, "frase larga")
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(sealed) || bytes.Contains(sealed, []byte("manifest")) {
		t.Fatal("not encrypted")
	}
	if _, err := Open(sealed, ""); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("no passphrase: %v", err)
	}
	if _, err := Open(sealed, "otra"); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	plain, err := Open(sealed, "frase larga")
	if err != nil || !bytes.Equal(plain, data) {
		t.Fatalf("open: %v", err)
	}
	if _, err := os.Stat(example); err != nil {
		t.Fatal(err)
	}
}
