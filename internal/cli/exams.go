package cli

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/examx"
	"github.com/lisk0vian/assess/internal/search"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/tui"
	"github.com/lisk0vian/assess/internal/vault"
)

// vaultExam is an exam package of the vault.
type vaultExam struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	HasKey   bool      `json:"has_key"`
	Files    int       `json:"files"`
	Packed   time.Time `json:"packed"`
	Sessions int       `json:"sessions"`
	LastUsed time.Time `json:"last_used,omitzero"`
	file     string
}

func examFile(id string) string { return vault.Join(vault.ExamsDir, vault.SafeName(id)+examx.Ext) }

// examIDs lists the exams of v by file name, without opening them.
func examIDs(v *vault.Vault) []string {
	names, _ := v.List(vault.ExamsDir)
	var out []string
	for _, n := range names {
		if id, ok := strings.CutSuffix(path.Base(n), examx.Ext); ok {
			out = append(out, id)
		}
	}
	return out
}

// readExam opens and verifies one exam package of v.
func readExam(v *vault.Vault, file string) (*examx.Contents, error) {
	data, err := v.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return examx.Read(data)
}

// vaultExams lists the exams of v; the vault must be open.
func vaultExams(v *vault.Vault) ([]*vaultExam, error) {
	names, err := v.List(vault.ExamsDir)
	if err != nil {
		return nil, err
	}
	var out []*vaultExam
	for _, n := range names {
		if !strings.HasSuffix(n, examx.Ext) {
			continue
		}
		c, err := readExam(v, n)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		out = append(out, &vaultExam{ID: c.Index.ExamID, Title: c.Index.Title, HasKey: c.Index.HasKey,
			Files: len(c.Files), Packed: c.Index.Created, file: n})
	}
	return out, nil
}

// withUsage fills in how often each exam was given.
func withUsage(exams []*vaultExam, sessions []*session.Overview) {
	for _, e := range exams {
		for _, s := range sessions {
			if s.ExamID == e.ID {
				e.Sessions++
				if s.CreatedAt.After(e.LastUsed) {
					e.LastUsed = s.CreatedAt
				}
			}
		}
	}
}

// storeExam packs the exam folder dir into the vault; an existing package
// of the same exam is replaced only with force.
func storeExam(v *vault.Vault, dir string, force bool) (*examx.Index, error) {
	data, idx, err := examx.Pack(dir, examx.Options{})
	if err != nil {
		return nil, err
	}
	if idx.ExamID == "" {
		return nil, errors.New(T("vault.add.no_id", "dir", dir))
	}
	file := examFile(idx.ExamID)
	if v.Exists(file) && !force {
		return nil, errors.New(T("vault.add.exists", "id", idx.ExamID))
	}
	return idx, v.WriteFile(file, data)
}

// extractExam unpacks a vault exam into a temporary folder.
func extractExam(v *vault.Vault, file string) (string, func(), error) {
	c, err := readExam(v, file)
	if err != nil {
		return "", func() {}, err
	}
	tmp, err := os.MkdirTemp("", "ax-exam-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if _, err := c.Extract(tmp, true); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmp, cleanup, nil
}

// findVaultExam resolves an exam of the vault by id, or asks which one.
func findVaultExam(v *vault.Vault, query string) (*vaultExam, error) {
	exams, err := vaultExams(v)
	if err != nil {
		return nil, err
	}
	for _, e := range exams {
		if strings.EqualFold(e.ID, query) {
			return e, nil
		}
	}
	hits := search.Rank(query, exams, func(e *vaultExam) []string { return []string{e.ID, e.Title} })
	return choose("exam", query, hits, func(e *vaultExam) tui.PickItem {
		return tui.PickItem{Key: e.ID, Label: e.Title}
	})
}

// examFolder turns what the teacher typed into an exam folder: a folder
// as it is, or an exam of the vault unpacked into a temporary folder.
func examFolder(cmd *cobra.Command, dataDir, ref string) (string, func(), error) {
	if info, err := os.Stat(ref); err == nil && info.IsDir() {
		return ref, func() {}, nil
	}
	v, _, err := activeVault(dataDir)
	if err != nil {
		return "", func() {}, err
	}
	if err := unlock(cmd.Context(), dataDir, v); err != nil {
		return "", func() {}, err
	}
	if len(examIDs(v)) == 0 {
		return "", func() {}, errors.New(T("vault.exam.not_found", "ref", ref))
	}
	e, err := findVaultExam(v, ref)
	if err != nil {
		return "", func() {}, err
	}
	return extractExam(v, e.file)
}

// ---------------- ax exams ----------------

func newExamsCmd() *cobra.Command {
	var asJSON, asCSV bool
	cmd := &cobra.Command{
		Use:   "exams",
		Short: T("vault.exams.short"),
		Long:  T("vault.exams.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			return withStore(cmd.Context(), dir, func(s *store) error {
				exams, err := vaultExams(s.vault)
				if err != nil {
					return err
				}
				sessions, err := s.svc.Sessions(cmd.Context())
				if err != nil {
					return err
				}
				withUsage(exams, sessions)
				sort.Slice(exams, func(i, j int) bool { return strings.ToLower(exams[i].Title) < strings.ToLower(exams[j].Title) })
				w := cmd.OutOrStdout()
				switch {
				case asJSON:
					enc := json.NewEncoder(w)
					enc.SetIndent("", "  ")
					return enc.Encode(exams)
				case asCSV:
					cw := csv.NewWriter(w)
					_ = cw.Write([]string{"id", "title", "has_key", "sessions", "last_used"})
					for _, e := range exams {
						_ = cw.Write([]string{e.ID, e.Title, strconv.FormatBool(e.HasKey), strconv.Itoa(e.Sessions), dateOrEmpty(e.LastUsed)})
					}
					cw.Flush()
					return cw.Error()
				}
				if len(exams) == 0 {
					fmt.Fprintln(w, T("vault.exams.empty"))
					return nil
				}
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, T("vault.exams.header"))
				for _, e := range exams {
					key := T("common.yes")
					if !e.HasKey {
						key = T("common.no")
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", e.ID, e.Title, key, e.Sessions, dateOrEmpty(e.LastUsed))
				}
				return tw.Flush()
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, T("cli.flag.json"))
	cmd.Flags().BoolVar(&asCSV, "csv", false, T("people.flag.csv"))
	return cmd
}

func dateOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02")
}
