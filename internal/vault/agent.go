package vault

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The unlock agent keeps an encrypted vault open for a while so the teacher
// types the password once: a small background process holding the key in
// memory, reachable on 127.0.0.1 with a token readable only by the
// teacher's account. It forgets the key after Idle without use, on
// `ax vault lock` and when the panel locks.

// AgentState is agents/<vault id>.json in the data folder.
type AgentState struct {
	PID   int    `json:"pid"`
	Addr  string `json:"addr"`
	Token string `json:"token"`
}

func agentFile(dataDir, vaultID string) string {
	return filepath.Join(dataDir, "agents", vaultID+".json")
}

// AgentKey asks the running agent of the vault for its key; "" when no
// agent holds it.
func AgentKey(ctx context.Context, dataDir, vaultID string) string {
	var out struct {
		Identity string `json:"identity"`
	}
	if err := agentCall(ctx, dataDir, vaultID, "/key", &out); err != nil {
		return ""
	}
	return out.Identity
}

// LockAgent stops the agent of the vault, if any.
func LockAgent(ctx context.Context, dataDir, vaultID string) bool {
	err := agentCall(ctx, dataDir, vaultID, "/lock", nil)
	_ = os.Remove(agentFile(dataDir, vaultID))
	return err == nil
}

// LockAllAgents stops every agent of the data folder (the panel's lock).
func LockAllAgents(ctx context.Context, dataDir string) {
	entries, _ := os.ReadDir(filepath.Join(dataDir, "agents"))
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			LockAgent(ctx, dataDir, id)
		}
	}
}

func agentCall(ctx context.Context, dataDir, vaultID, path string, out any) error {
	data, err := os.ReadFile(agentFile(dataDir, vaultID))
	if err != nil {
		return err
	}
	var st AgentState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+st.Addr+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+st.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_ = os.Remove(agentFile(dataDir, vaultID)) // a dead agent
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// SpawnAgent starts `exe vault agent` in the background and hands it the
// key through a pipe: the key never reaches the command line or the disk.
func SpawnAgent(exe, dataDir string, v *Vault, idle time.Duration, detach func(*exec.Cmd)) error {
	if v.Locked() || !v.Encrypted() || idle <= 0 {
		return nil
	}
	cmd := exec.Command(exe, "vault", "agent", "--data-dir", dataDir, "--id", v.Meta.ID, "--idle", idle.String()) //nolint:gosec // G204: re-executes ourselves
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if detach != nil {
		detach(cmd)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, v.Identity()+"\n"); err != nil {
		return err
	}
	_ = stdin.Close()
	// The agent prints "ready" once it is listening.
	line, err := bufio.NewReader(stdout).ReadString('\n')
	_ = cmd.Process.Release()
	if err != nil || strings.TrimSpace(line) != "ready" {
		return errors.New("the unlock agent did not start")
	}
	return nil
}

// RunAgent is the agent process: it reads the key from in, writes "ready"
// to out and serves until idle passes without a request, a lock request
// arrives or ctx ends.
func RunAgent(ctx context.Context, dataDir, vaultID string, idle time.Duration, in io.Reader, out io.Writer) error {
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return errors.New("no key on standard input")
	}
	identity := strings.TrimSpace(line)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		return err
	}
	st := AgentState{PID: os.Getpid(), Addr: ln.Addr().String(), Token: hex.EncodeToString(tok)}
	path := agentFile(dataDir, vaultID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	defer func() {
		// Only remove the file if it is still ours.
		if cur, err := os.ReadFile(path); err == nil && strings.Contains(string(cur), st.Token) {
			_ = os.Remove(path)
		}
	}()

	var mu sync.Mutex
	last := time.Now()
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	auth := func(r *http.Request) bool {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		return subtle.ConstantTimeCompare([]byte(got), []byte(st.Token)) == 1
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /key", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mu.Lock()
		last = time.Now()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"identity": identity})
	})
	mux.HandleFunc("POST /lock", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		stop()
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	fmt.Fprintln(out, "ready")

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-done:
			break loop
		case <-tick.C:
			mu.Lock()
			expired := time.Since(last) > idle
			mu.Unlock()
			if expired {
				break loop
			}
		}
	}
	sctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}
