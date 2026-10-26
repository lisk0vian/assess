package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/lisk0vian/assess/internal/archive"
	"github.com/lisk0vian/assess/internal/backup"
	"github.com/lisk0vian/assess/internal/datadir"
	"github.com/lisk0vian/assess/internal/netutil"
	"github.com/lisk0vian/assess/internal/seal"
	"github.com/lisk0vian/assess/internal/server/student"
	"github.com/lisk0vian/assess/internal/server/web"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
	"github.com/lisk0vian/assess/internal/vault"
)

// Config configures a daemon.
type Config struct {
	DataDir      string
	Host         string // listen address for students; "" means all interfaces
	Port         int
	PortFallback bool          // try the next ports when Port is busy
	JoinWindow   time.Duration // close joining this long after the start; 0 keeps it open
	AllowPublic  bool          // accept students from non-private addresses
	Version      string
	Log          *slog.Logger
	// VaultDir holds the database and receives the session's archive; empty
	// uses the data folder's database and archives nothing (tests).
	VaultDir string
	// DBKey opens an encrypted vault's database; it arrives through a pipe.
	DBKey []byte
	// KeepBackups is how many automatic backups to keep (0 makes none).
	KeepBackups int
}

type daemon struct {
	cfg        Config
	db         *sqlite.DB
	vault      *vault.Vault
	archived   bool
	svc        *session.Service
	students   *student.Handler
	sessionID  string
	examID     string
	examTitle  string
	className  string
	classID    string
	studentURL string
	otherURLs  []string
	startedAt  time.Time
	stop       chan struct{}
	stopOnce   sync.Once

	mu      sync.Mutex
	lastErr string
}

func (d *daemon) requestStop() { d.stopOnce.Do(func() { close(d.stop) }) }

func (d *daemon) setError(err error) {
	d.mu.Lock()
	d.lastErr = err.Error()
	d.mu.Unlock()
	d.cfg.Log.Error("daemon error", "err", err)
}

func (d *daemon) lastError() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastErr
}

// Run serves the open session of cfg.DataDir until ctx ends or a shutdown is
// requested through the control API.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	dbPath := datadir.DB(cfg.DataDir)
	var v *vault.Vault
	var cipher *seal.Cipher
	if cfg.VaultDir != "" {
		var err error
		if v, err = vault.Open(cfg.VaultDir); err != nil {
			return err
		}
		dbPath = v.DB()
		if v.Encrypted() {
			if len(cfg.DBKey) == 0 {
				return errors.New("the vault is encrypted and no key was given")
			}
			if cipher, err = seal.New(cfg.DBKey); err != nil {
				return err
			}
		}
	}
	db, err := sqlite.Open(ctx, dbPath, sqlite.WithCipher(cipher, v != nil && v.Encrypted()))
	if err != nil {
		return err
	}
	defer db.Close()
	// Changes reach the browsers through the student handler, created below.
	var students *student.Handler
	svc := session.NewService(db, session.WithNotify(func(id string) {
		if students != nil {
			students.Notify(id)
		}
	}))

	sess, err := svc.OpenSession(ctx)
	if err != nil {
		return fmt.Errorf("no open session to serve: %w", err)
	}
	_, pub, err := svc.Session(ctx, sess.ID)
	if err != nil {
		return err
	}
	d := &daemon{
		cfg: cfg, db: db, vault: v, svc: svc, sessionID: sess.ID, examID: pub.Exam.Manifest.ID, examTitle: pub.Exam.Manifest.Title,
		startedAt: time.Now(), stop: make(chan struct{}),
	}
	d.classID = sess.ClassID
	if sess.ClassID != "" {
		if p, err := svc.People(ctx); err == nil {
			for _, c := range p.Classes {
				if c.ID == sess.ClassID {
					d.className = c.Name
				}
			}
		}
	}
	// Recover sessions that expired while no server was running.
	if n, err := svc.FinalizeExpired(ctx); err != nil {
		return err
	} else if n > 0 {
		cfg.Log.Info("finalized expired attempts on start", "count", n)
	}

	studentLn, port, err := netutil.Listen(cfg.Host, cfg.Port, cfg.PortFallback)
	if err != nil {
		return err
	}
	controlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		studentLn.Close()
		return err
	}
	d.studentURL, d.otherURLs = urlsFor(cfg.Host, port)

	students = student.New(svc, sess.ID, student.Options{JoinWindow: cfg.JoinWindow, Log: cfg.Log})
	d.students = students
	mux := http.NewServeMux()
	lang := pub.Exam.Manifest.Lang()
	mux.Handle("/api/", students)
	mux.Handle("GET /api/i18n", web.StringsHandler(lang))
	mux.Handle("/", web.Handler(lang))
	var root http.Handler = mux
	if !cfg.AllowPublic {
		root = student.PrivateNetworksOnly(mux)
	}
	studentSrv := &http.Server{Handler: root, ReadHeaderTimeout: 10 * time.Second}

	token := randomToken()
	controlSrv := &http.Server{Handler: (&controlServer{d: d, token: token}).routes(), ReadHeaderTimeout: 5 * time.Second}

	state := &State{
		PID: os.Getpid(), Version: cfg.Version, SessionID: sess.ID, ExamTitle: d.examTitle, Code: sess.Code,
		StudentURL: d.studentURL, OtherURLs: d.otherURLs, ControlAddr: controlLn.Addr().String(), Token: token,
		StartedAt: d.startedAt, Heartbeat: time.Now(),
	}
	if err := writeState(cfg.DataDir, state); err != nil {
		return err
	}
	defer removeState(cfg.DataDir)

	errc := make(chan error, 2)
	go func() { errc <- studentSrv.Serve(studentLn) }()
	go func() { errc <- controlSrv.Serve(controlLn) }()
	cfg.Log.Info("serving", "session", sess.ID, "url", d.studentURL, "control", state.ControlAddr)

	heartbeat := time.NewTicker(HeartbeatEvery)
	defer heartbeat.Stop()
	finalize := time.NewTicker(time.Second)
	defer finalize.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-d.stop:
			break loop
		case err := <-errc:
			if !errors.Is(err, http.ErrServerClosed) {
				d.setError(err)
				break loop
			}
		case <-heartbeat.C:
			state.Heartbeat = time.Now()
			if err := writeState(cfg.DataDir, state); err != nil {
				d.setError(err)
			}
		case <-finalize.C:
			if _, err := svc.FinalizeExpired(ctx); err != nil {
				d.setError(err)
			}
			d.archiveIfEnded(ctx)
		}
	}
	d.archiveIfEnded(context.Background())

	cfg.Log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = studentSrv.Shutdown(shutdownCtx)
	_ = controlSrv.Shutdown(shutdownCtx)
	return nil
}

// archiveIfEnded writes the session's archive into the vault, and an
// automatic backup, once the session has ended.
func (d *daemon) archiveIfEnded(ctx context.Context) {
	if d.vault == nil || d.archived {
		return
	}
	sess, _, err := d.svc.Session(ctx, d.sessionID)
	if err != nil || sess.State != session.StateEnded {
		return
	}
	d.archived = true
	folder, err := archive.Write(ctx, d.vault, d.svc, d.sessionID)
	if err != nil {
		d.setError(fmt.Errorf("archive: %w", err))
		return
	}
	d.cfg.Log.Info("session archived", "folder", folder)
	if d.cfg.KeepBackups <= 0 {
		return
	}
	file, err := backup.Create(ctx, d.vault, d.db, "", backup.AutoPrefix)
	if err != nil {
		d.setError(fmt.Errorf("backup: %w", err))
		return
	}
	d.cfg.Log.Info("backup written", "file", file)
	if err := backup.Prune(d.vault, d.cfg.KeepBackups); err != nil {
		d.cfg.Log.Warn("pruning backups", "err", err)
	}
}

// urlsFor builds the address students type. Listening on all interfaces
// advertises the best LAN address and lists the others.
func urlsFor(host string, port int) (string, []string) {
	p := strconv.Itoa(port)
	if host != "" && host != "0.0.0.0" && host != "::" {
		return "http://" + net.JoinHostPort(host, p) + "/", nil
	}
	ips := netutil.LANAddrs()
	if len(ips) == 0 {
		return "http://localhost:" + p + "/", nil
	}
	var urls []string
	for _, ip := range ips {
		urls = append(urls, "http://"+net.JoinHostPort(ip.String(), p)+"/")
	}
	return urls[0], urls[1:]
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Spawn starts the daemon for dir as a detached background process running
// `ax daemon` with args, and waits until it reports itself running. A key
// is written to the daemon's standard input, never to its command line.
func Spawn(dir string, args []string, key []byte) (*State, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logf, err := OpenLog(dir)
	if err != nil {
		return nil, err
	}
	defer logf.Close()

	if key != nil {
		args = append(args, "--key-stdin")
	}
	cmd := exec.Command(exe, append([]string{"daemon", "--data-dir", dir}, args...)...) //nolint:gosec // G204: re-executes ourselves
	cmd.Stdout, cmd.Stderr = logf, logf
	if key != nil {
		cmd.Stdin = bytes.NewReader(key)
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.After(15 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			return nil, fmt.Errorf("the exam server exited during start-up (%w); see `ax logs`", err)
		case <-deadline:
			return nil, errors.New("the exam server did not start within 15 s; see `ax logs`")
		case <-tick.C:
			if h, st, _ := Check(dir); h == Running && st.PID == cmd.Process.Pid {
				_ = cmd.Process.Release()
				return st, nil
			}
		}
	}
}

// OpenLog opens the daemon log for appending, rotating it beyond 5 MiB.
func OpenLog(dir string) (*os.File, error) {
	path := datadir.Log(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > 5<<20 {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
