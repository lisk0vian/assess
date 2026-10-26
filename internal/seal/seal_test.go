package seal

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, KeySize) }

func TestRoundTrip(t *testing.T) {
	c, err := New(key(1))
	if err != nil {
		t.Fatal(err)
	}
	blob := c.Blob([]byte(`{"a":1}`))
	if !IsSealed(blob) || bytes.Contains(blob, []byte(`"a"`)) {
		t.Fatalf("blob not sealed: %q", blob)
	}
	if got, err := c.OpenBlob(blob); err != nil || string(got) != `{"a":1}` {
		t.Fatalf("OpenBlob = %q, %v", got, err)
	}
	txt := c.Text("Ana García")
	if strings.Contains(txt, "Ana") || txt == c.Text("Ana García") {
		t.Fatalf("Text must hide the value and use a fresh nonce: %q", txt)
	}
	if got, _ := c.OpenText(txt); got != "Ana García" {
		t.Fatalf("OpenText = %q", got)
	}
	d1, d2 := c.Det("A-12"), c.Det("A-12")
	if d1 != d2 || d1 == c.Det("A-13") {
		t.Fatalf("Det must be stable and distinct: %q %q", d1, d2)
	}
	if got, _ := c.OpenText(d1); got != "A-12" {
		t.Fatalf("OpenText(det) = %q", got)
	}
}

func TestPlainPassesThrough(t *testing.T) {
	var c *Cipher
	if got := c.Text("x"); got != "x" {
		t.Fatal(got)
	}
	if got, err := c.OpenText("plain"); err != nil || got != "plain" {
		t.Fatal(got, err)
	}
	sealed, _ := New(key(2))
	if _, err := c.OpenText(sealed.Text("x")); !errors.Is(err, ErrLocked) {
		t.Fatalf("reading a sealed value without the key: %v", err)
	}
	if _, err := c.OpenBlob(sealed.Blob([]byte("x"))); !errors.Is(err, ErrLocked) {
		t.Fatalf("reading a sealed blob without the key: %v", err)
	}
}

func TestWrongKey(t *testing.T) {
	a, _ := New(key(3))
	b, _ := New(key(4))
	if _, err := b.OpenText(a.Text("x")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}
