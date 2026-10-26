package cli

import (
	"archive/zip"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/session"
)

// gradebook is a class's students × exams table of percentages: the
// latest submitted attempt of each exam.
type gradebook struct {
	Class    string          `json:"class"`
	Exams    []gradebookExam `json:"exams"`
	Students []gradebookRow  `json:"students"`
}

type gradebookExam struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	First time.Time `json:"first"`
}

type gradebookRow struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	Percent map[string]*float64 `json:"percent"` // by exam id; nil: absent or not submitted
	Average *float64            `json:"average"`
}

func buildGradebook(cmd *cobra.Command, svc *session.Service, className string) (*gradebook, error) {
	ctx := cmd.Context()
	p, err := svc.People(ctx)
	if err != nil {
		return nil, err
	}
	c, err := findClass(p, className)
	if err != nil {
		return nil, err
	}
	gb := &gradebook{Class: c.Name}
	exams := map[string]*gradebookExam{}
	for _, id := range p.Members[c.ID] {
		st := p.ByID[id]
		h, err := svc.History(ctx, st.Ref)
		if err != nil {
			return nil, err
		}
		row := gradebookRow{ID: st.Ref, Name: st.Name, Percent: map[string]*float64{}}
		latest := map[string]time.Time{}
		for _, x := range h {
			e := exams[x.ExamID]
			if e == nil {
				e = &gradebookExam{ID: x.ExamID, Title: x.ExamTitle, First: x.Session.CreatedAt}
				exams[x.ExamID] = e
			}
			if x.Session.CreatedAt.Before(e.First) {
				e.First = x.Session.CreatedAt
			}
			a := x.Attempt
			if a == nil || a.SubmittedAt.IsZero() || a.State != session.AttemptSubmitted || a.MaxScore == 0 {
				continue
			}
			if !x.Session.CreatedAt.After(latest[x.ExamID]) {
				continue
			}
			latest[x.ExamID] = x.Session.CreatedAt
			pc := round1(a.Score / a.MaxScore * 100)
			row.Percent[x.ExamID] = &pc
		}
		gb.Students = append(gb.Students, row)
	}
	for _, e := range exams {
		gb.Exams = append(gb.Exams, *e)
	}
	sort.Slice(gb.Exams, func(i, j int) bool { return gb.Exams[i].First.Before(gb.Exams[j].First) })
	for i := range gb.Students {
		var sum float64
		n := 0
		for _, e := range gb.Exams {
			if v := gb.Students[i].Percent[e.ID]; v != nil {
				sum += *v
				n++
			}
		}
		if n > 0 {
			avg := round1(sum / float64(n))
			gb.Students[i].Average = &avg
		}
	}
	return gb, nil
}

// table is the gradebook as rows of text, header first.
func (gb *gradebook) table() [][]string {
	head := []string{T("gradebook.col.id"), T("gradebook.col.name")}
	for _, e := range gb.Exams {
		head = append(head, e.Title)
	}
	head = append(head, T("gradebook.col.average"))
	out := [][]string{head}
	f := func(v *float64) string {
		if v == nil {
			return ""
		}
		return strconv.FormatFloat(*v, 'f', -1, 64)
	}
	for _, r := range gb.Students {
		rec := []string{r.ID, r.Name}
		for _, e := range gb.Exams {
			rec = append(rec, f(r.Percent[e.ID]))
		}
		out = append(out, append(rec, f(r.Average)))
	}
	return out
}

func newGradebookCmd() *cobra.Command {
	var asJSON, asCSV, asXLSX bool
	var out, sep string
	cmd := &cobra.Command{
		Use:               "gradebook <class>",
		Short:             T("gradebook.short"),
		Long:              T("gradebook.long"),
		Example:           T("gradebook.example"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClasses,
		RunE: func(cmd *cobra.Command, args []string) error {
			if out != "" && strings.EqualFold(filepath.Ext(out), ".xlsx") {
				asXLSX = true
			}
			if asXLSX && out == "" {
				return errors.New(T("gradebook.xlsx_needs_out"))
			}
			return peopleCmd(cmd, func(s *store) error {
				gb, err := buildGradebook(cmd, s.svc, args[0])
				if err != nil {
					return err
				}
				w := cmd.OutOrStdout()
				if out != "" {
					f, err := os.Create(out)
					if err != nil {
						return err
					}
					defer f.Close()
					w = f
				}
				switch {
				case asXLSX:
					err = writeXLSX(w, gb.Class, gb.table())
				case asJSON:
					err = (&listFormat{json: true}).write(w, gb, nil, nil)
				case asCSV || out != "":
					if cfg.Bool("results.bom") && out != "" {
						_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
					}
					cw := csv.NewWriter(w)
					cw.Comma = []rune(sep)[0]
					err = cw.WriteAll(gb.table())
				default:
					tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
					for _, r := range gb.table() {
						fmt.Fprintln(tw, strings.Join(r, "\t"))
					}
					err = tw.Flush()
				}
				if err == nil && out != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), T("gradebook.wrote", "n", len(gb.Students), "exams", len(gb.Exams), "file", out))
				}
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, T("cli.flag.json"))
	cmd.Flags().BoolVar(&asCSV, "csv", false, T("people.flag.csv"))
	cmd.Flags().BoolVar(&asXLSX, "xlsx", false, T("gradebook.flag_xlsx"))
	cmd.Flags().StringVarP(&out, "out", "o", "", T("cli.results.flag_out"))
	cmd.Flags().StringVar(&sep, "sep", cfg.Str("results.separator"), T("cli.results.flag_sep"))
	return cmd
}

// writeXLSX writes rows as a one-sheet Excel workbook; cells that parse as
// numbers are stored as numbers.
func writeXLSX(w io.Writer, sheet string, rows [][]string) error {
	zw := zip.NewWriter(w)
	put := func(name, body string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+"\n"+body)
		return err
	}
	esc := func(s string) string {
		var b strings.Builder
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	if len([]rune(sheet)) > 31 {
		sheet = string([]rune(sheet)[:31])
	}
	sheet = strings.NewReplacer("/", "-", `\`, "-", "?", "", "*", "", "[", "(", "]", ")", ":", "-").Replace(sheet)
	var data strings.Builder
	for r, row := range rows {
		fmt.Fprintf(&data, `<row r="%d">`, r+1)
		for c, v := range row {
			ref := colName(c) + strconv.Itoa(r+1)
			style := ""
			if r == 0 {
				style = ` s="1"`
			}
			if _, err := strconv.ParseFloat(v, 64); err == nil && r > 0 && c > 1 {
				fmt.Fprintf(&data, `<c r="%s"><v>%s</v></c>`, ref, v)
			} else if v != "" {
				fmt.Fprintf(&data, `<c r="%s" t="inlineStr"%s><is><t>%s</t></is></c>`, ref, style, esc(v))
			}
		}
		data.WriteString(`</row>`)
	}
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`</Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets><sheet name="` + esc(sheet) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
			`</Relationships>`},
		{"xl/styles.xml", `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
			`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
			`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
			`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
			`<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs>` +
			`</styleSheet>`},
		{"xl/worksheets/sheet1.xml", `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>` +
			`<sheetData>` + data.String() + `</sheetData></worksheet>`},
	}
	for _, p := range parts {
		if err := put(p.name, p.body); err != nil {
			return err
		}
	}
	return zw.Close()
}

// colName turns 0, 1, … 26 into A, B, … AA.
func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}
