package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The teacher sits the exam in a rehearsal: join, submit, see the grade,
// and nothing is kept.
func TestTryExam(t *testing.T) {
	urls := make(chan string, 1)
	old := openBrowser
	openBrowser = func(u string) error { urls <- u; return nil }
	t.Cleanup(func() { openBrowser = old })

	cmd := newRootCmd()
	var out bytes.Buffer
	in, enter := io.Pipe()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(in)
	data := t.TempDir()
	cmd.SetArgs([]string{"exam", "try", "../../examples/algebra-01", "--data-dir", data})
	errc := make(chan error, 1)
	go func() { errc <- cmd.Execute() }()

	var page string
	select {
	case page = <-urls:
	case err := <-errc:
		t.Fatalf("ended before serving: %v\n%s", err, out.String())
	case <-time.After(10 * time.Second):
		t.Fatal("the browser was never opened")
	}
	u, _ := url.Parse(page)
	if u.Hostname() != "127.0.0.1" {
		t.Fatalf("a rehearsal serves this computer only: %s", page)
	}
	base := u.Scheme + "://" + u.Host
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	post := func(path, body string) {
		t.Helper()
		res, err := c.Post(base+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.StatusCode, b)
		}
	}
	post("/api/join", `{"code":"`+u.Query().Get("c")+`","name":"Profe","student_id":"yo"}`)
	post("/api/submit", `{}`)

	_ = enter.Close() // Enter in the terminal
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "Your try: 0 of 9 points") || !strings.Contains(s, "nothing was kept") {
		t.Fatalf("verdict:\n%s", s)
	}
	if resp, err := http.Get(base); err == nil {
		resp.Body.Close()
		t.Fatal("the rehearsal server must be gone")
	}
}

// Without an exam, the rehearsal opens the example exam.
func TestTryExampleExam(t *testing.T) {
	old := openBrowser
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { openBrowser = old })
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("\n"))
	cmd.SetArgs([]string{"exam", "try", "--data-dir", t.TempDir()})
	if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "You did not submit it.") {
		t.Fatalf("%v\n%s", err, out.String())
	}
}
