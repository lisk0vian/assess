// Package archive writes the record of a finished session into the vault:
// a readable summary, the results as CSV and the complete record as JSON,
// in sessions/YYYY/<date> <exam> <class>/.
package archive

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
	"github.com/lisk0vian/assess/internal/report"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/vault"
)

// File names inside a session folder.
const (
	SummaryFile = "summary.md"
	ResultsFile = "results.csv"
	RecordFile  = "record.json"
)

// historyLimit bounds how many earlier sessions feed the report.
const historyLimit = 20

// Record is record.json: everything about the session, readable without ax.
type Record struct {
	Format  int                          `json:"format"`
	Written time.Time                    `json:"written"`
	Class   string                       `json:"class,omitempty"`
	Exam    *exam.Exam                   `json:"exam"` // as published, answer key included
	Results *session.Results             `json:"results"`
	Report  *report.Report               `json:"report"`
	Events  []*session.Event             `json:"events"`
	Answers map[string][]*session.Answer `json:"answers"` // by student id
}

// Folder is the logical vault folder of a session's archive.
func Folder(started time.Time, title, class string) string {
	when := started.Local()
	name := when.Format("2006-01-02 1504") + " " + title
	if class != "" {
		name += " " + class
	}
	return vault.Join(vault.SessionDir, when.Format("2006"), vault.SafeName(name))
}

// BuildReport analyses sessionID with the earlier finished sessions of the
// same exam.
func BuildReport(ctx context.Context, svc *session.Service, sessionID string) (*report.Report, *session.Archive, error) {
	cur, err := svc.Archive(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	all, err := svc.ExamSessions(ctx, cur.Published.Exam.Manifest.ID)
	if err != nil {
		return nil, nil, err
	}
	var earlier []*session.Archive
	for _, s := range all {
		if s.ID == cur.Session.ID || s.State != session.StateEnded || s.StartedAt.IsZero() || !s.StartedAt.Before(cur.Session.StartedAt) {
			continue
		}
		arc, err := svc.Archive(ctx, s.ID)
		if err != nil {
			return nil, nil, err
		}
		earlier = append(earlier, arc)
	}
	if len(earlier) > historyLimit {
		earlier = earlier[len(earlier)-historyLimit:]
	}
	return report.Build(cur, earlier), cur, nil
}

// Write stores the archive of the ended session sessionID in v and records
// its folder. Writing again replaces it (after a student is forgotten).
func Write(ctx context.Context, v *vault.Vault, svc *session.Service, sessionID string) (string, error) {
	rep, arc, err := BuildReport(ctx, svc, sessionID)
	if err != nil {
		return "", err
	}
	if arc.Session.State != session.StateEnded {
		return "", fmt.Errorf("session %s has not ended", arc.Session.Code)
	}
	res, err := svc.Results(ctx, sessionID)
	if err != nil {
		return "", err
	}
	class := ""
	if arc.Session.ClassID != "" {
		if p, err := svc.People(ctx); err == nil {
			for _, c := range p.Classes {
				if c.ID == arc.Session.ClassID {
					class = c.Name
				}
			}
		}
	}
	started := arc.Session.StartedAt
	if started.IsZero() {
		started = arc.Session.CreatedAt
	}
	folder := arc.Session.ArchivedAs
	if folder == "" {
		base := Folder(started, arc.Published.Exam.Manifest.Title, class)
		folder = base
		for n := 2; v.Exists(vault.Join(folder, RecordFile)); n++ {
			folder = fmt.Sprintf("%s (%d)", base, n)
		}
	}

	var csvBuf bytes.Buffer
	csvBuf.Write([]byte{0xEF, 0xBB, 0xBF}) // lets spreadsheets read accents
	if err := CSV(&csvBuf, res, ','); err != nil {
		return "", err
	}
	answers := map[string][]*session.Answer{}
	for _, a := range arc.Attempts {
		if p := arc.Participants[a.ParticipantID]; p != nil {
			answers[p.StudentID] = arc.Answers[a.ID]
		}
	}
	rec := &Record{Format: 1, Written: time.Now().UTC().Truncate(time.Second), Class: class, Exam: arc.Published.Exam,
		Results: res, Report: rep, Events: arc.Events, Answers: answers}
	recJSON, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", err
	}
	files := map[string][]byte{
		SummaryFile: []byte(Summary(rep, class)),
		ResultsFile: csvBuf.Bytes(),
		RecordFile:  recJSON,
	}
	for name, data := range files {
		if err := v.WriteFile(vault.Join(folder, name), data); err != nil {
			return "", err
		}
	}
	if err := svc.SetArchive(ctx, sessionID, folder); err != nil {
		return "", err
	}
	return folder, nil
}

// Missing archives every ended session that has no archive yet and returns
// how many it wrote.
func Missing(ctx context.Context, v *vault.Vault, svc *session.Service) (int, error) {
	all, err := svc.Sessions(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range all {
		if s.State != session.StateEnded || (s.ArchivedAs != "" && v.Exists(vault.Join(s.ArchivedAs, RecordFile))) {
			continue
		}
		if _, err := Write(ctx, v, svc, s.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// CSV writes the results table: one row per student, one column per
// question.
func CSV(w io.Writer, res *session.Results, sep rune) error {
	cw := csv.NewWriter(w)
	cw.Comma = sep
	header := []string{"student_id", "name", "state", "score", "max_score", "percent", "submitted_at", "submit_reason", "minutes"}
	header = append(header, res.Questions...)
	if err := cw.Write(header); err != nil {
		return err
	}
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	for _, r := range res.Rows {
		rec := []string{r.StudentID, r.Name, string(r.State), "", num(r.MaxScore), "", "", string(r.SubmitReason), ""}
		if r.Score != nil {
			rec[3] = num(*r.Score)
			if r.MaxScore > 0 {
				rec[5] = num(float64(int(*r.Score/r.MaxScore*1000+0.5)) / 10)
			}
		}
		if !r.SubmittedAt.IsZero() {
			rec[6] = r.SubmittedAt.Local().Format(time.DateTime)
			rec[8] = num(float64(int(r.Minutes*10+0.5)) / 10)
		}
		for _, q := range res.Questions {
			if p, ok := r.Points[q]; ok {
				rec = append(rec, num(p))
			} else {
				rec = append(rec, "") // the question was not in this student's exam (pools)
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// Summary renders the readable summary.md of a report.
func Summary(r *report.Report, class string) string {
	T := i18n.T
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", r.Session.ExamTitle)
	line := func(k, v string) { fmt.Fprintf(&b, "- **%s:** %s\n", k, v) }
	if !r.Session.StartedAt.IsZero() {
		line(T("archive.date"), r.Session.StartedAt.Local().Format("2006-01-02 15:04"))
	}
	if class != "" {
		line(T("archive.class"), class)
	}
	line(T("archive.code"), r.Session.Code)
	line(T("archive.duration"), T("archive.minutes", "n", fmtNum(r.Session.DurationMin)))
	s := r.Summary
	line(T("archive.students"), T("archive.students_value", "students", s.Students, "submitted", s.Submitted,
		"annulled", s.Annulled, "withdrawn", s.Withdrawn))
	if s.Submitted > 0 {
		line(T("archive.scores"), T("archive.scores_value", "mean", fmtNum(s.Mean), "median", fmtNum(s.Median),
			"min", fmtNum(s.Min), "max", fmtNum(s.Max)))
	}
	if r.Incidents.Strikes > 0 || r.Incidents.Reverted > 0 {
		line(T("archive.integrity"), T("archive.integrity_value", "strikes", r.Incidents.Strikes, "reverted", r.Incidents.Reverted))
	}

	fmt.Fprintf(&b, "\n## %s\n\n", T("archive.students"))
	fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n|---|---|---|---:|---:|---:|\n",
		T("archive.col.name"), T("archive.col.id"), T("archive.col.state"), T("archive.col.percent"), T("archive.col.strikes"), T("archive.col.signals"))
	students := append([]report.StudentLine{}, r.Students...)
	sort.SliceStable(students, func(i, j int) bool { return students[i].Name < students[j].Name })
	for _, st := range students {
		pct := "—"
		if st.Percent != nil {
			pct = fmtNum(*st.Percent)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %d |\n", md(st.Name), md(st.StudentID), T("astate."+st.State), pct, st.Strikes, st.Signals)
	}

	if len(r.Questions) > 0 {
		fmt.Fprintf(&b, "\n## %s\n\n", T("archive.questions"))
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n|---|---|---:|---:|\n", T("archive.col.question"), T("archive.col.prompt"), T("archive.col.seen"), T("archive.col.rate"))
		for _, q := range r.Questions {
			flag := ""
			if q.Leak {
				flag = " ⚠"
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %s%%%s |\n", md(q.ID), md(q.Prompt), q.Seen, fmtNum(q.Rate*100), flag)
		}
	}
	if len(r.Pairs) > 0 {
		fmt.Fprintf(&b, "\n%s\n", T("archive.pairs", "n", len(r.Pairs)))
	}
	return b.String()
}

func fmtNum(f float64) string { return strconv.FormatFloat(float64(int(f*10+0.5))/10, 'f', -1, 64) }

// md keeps a table cell on one line and its pipes literal.
func md(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ", "\r", "").Replace(s)
}

// ClassFile is the readable copy of a class in the vault.
func ClassFile(name string) string {
	return vault.Join(vault.ClassesDir, vault.SafeName(name)+".yaml")
}

// WriteClass rewrites the readable copy of a class in classes/.
func WriteClass(ctx context.Context, v *vault.Vault, svc *session.Service, classID string) error {
	p, err := svc.People(ctx)
	if err != nil {
		return err
	}
	for _, c := range p.Classes {
		if c.ID != classID {
			continue
		}
		type member struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
		}
		doc := struct {
			Name     string   `yaml:"name"`
			Students []member `yaml:"students"`
		}{Name: c.Name, Students: []member{}}
		for _, id := range p.Members[c.ID] {
			st := p.ByID[id]
			doc.Students = append(doc.Students, member{ID: st.Ref, Name: st.Name})
		}
		data, err := yaml.Marshal(doc)
		if err != nil {
			return err
		}
		head := "# " + i18n.T("people.class_file_header") + "\n"
		return v.WriteFile(ClassFile(c.Name), append([]byte(head), data...))
	}
	return nil
}
