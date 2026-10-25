package exam

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Draft is an exam folder opened for editing. Every change is written to
// disk at once and the folder is loaded and validated again, so the draft
// always shows what is on disk.
//
// Files are merged rather than regenerated: comments, key order, quoting of
// unchanged values and blank lines between top-level sections survive.
type Draft struct {
	Dir      string
	Exam     *Exam
	Problems Problems
}

// OpenDraft loads the exam folder at dir for editing.
func OpenDraft(dir string) (*Draft, error) {
	d := &Draft{Dir: dir}
	return d, d.Reload()
}

// Reload reads the folder again, for example after an external editor.
func (d *Draft) Reload() error {
	ex, probs, err := Load(d.Dir)
	if err != nil {
		return err
	}
	d.Exam, d.Problems = ex, probs
	return nil
}

// ErrBrokenFile is returned when a file to update cannot be parsed; it must
// be fixed by hand (or in $EDITOR) first.
var ErrBrokenFile = errors.New("the file has YAML syntax errors; fix it first")

// SaveManifest writes m to manifest.yaml.
func (d *Draft) SaveManifest(m Manifest) error {
	if err := d.write(ManifestFile, manifestNode(&m)); err != nil {
		return err
	}
	return d.Reload()
}

// SaveQuestion writes q to its file and a to the answer key. A new question
// is also appended to the manifest's items.
func (d *Draft) SaveQuestion(q *Question, a Answer) error {
	if !ValidID(q.ID) {
		return fmt.Errorf("invalid question id %q", q.ID)
	}
	isNew := d.Exam.Questions[q.ID] == nil
	if err := d.write(QuestionsDir+"/"+q.ID+".yaml", questionNode(q)); err != nil {
		return err
	}
	key := d.Exam.Key
	if key.Format == "" {
		key.Format = FormatV1
	}
	if key.Exam == "" {
		key.Exam = d.Exam.Manifest.ID
	}
	answers := make(map[string]Answer, len(key.Answers)+1)
	for id, v := range key.Answers {
		answers[id] = v
	}
	answers[q.ID] = a
	key.Answers = answers
	if err := d.write(AnswerKeyFile, keyNode(&key, d.keyOrder())); err != nil {
		return err
	}
	if isNew && !d.referenced(q.ID) {
		m := d.Exam.Manifest
		m.Items = append(slices.Clone(m.Items), Item{Question: q.ID})
		if err := d.write(ManifestFile, manifestNode(&m)); err != nil {
			return err
		}
	}
	return d.Reload()
}

// DeleteQuestion removes the question file, its answer and every reference
// in the items. A pool keeps leaving at least one question out: its pick
// shrinks, and a pool left with one question becomes a plain item.
func (d *Draft) DeleteQuestion(id string) error {
	q := d.Exam.Questions[id]
	if q == nil {
		return fmt.Errorf("no question %q", id)
	}
	m := d.Exam.Manifest
	var items []Item
	for _, it := range m.Items {
		switch {
		case it.Pool != nil:
			p := *it.Pool
			p.From = slices.DeleteFunc(slices.Clone(p.From), func(s string) bool { return s == id })
			switch len(p.From) {
			case 0:
			case 1: // a pool must leave a question out: one left is a plain question
				items = append(items, Item{Question: p.From[0]})
			default:
				p.Pick = min(p.Pick, len(p.From)-1)
				items = append(items, Item{Pool: &p})
			}
		case it.Question != id:
			items = append(items, it)
		}
	}
	m.Items = items
	if err := d.write(ManifestFile, manifestNode(&m)); err != nil {
		return err
	}
	if _, ok := d.Exam.Key.Answers[id]; ok {
		key := d.Exam.Key
		key.Answers = map[string]Answer{}
		for k, v := range d.Exam.Key.Answers {
			if k != id {
				key.Answers[k] = v
			}
		}
		if err := d.write(AnswerKeyFile, keyNode(&key, d.keyOrder())); err != nil {
			return err
		}
	}
	file := q.File
	if file == "" {
		file = QuestionsDir + "/" + id + ".yaml"
	}
	if err := os.Remove(d.abs(file)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return d.Reload()
}

// MoveItem moves the item at index i by delta places (-1 up, +1 down).
func (d *Draft) MoveItem(i, delta int) error {
	items := slices.Clone(d.Exam.Manifest.Items)
	j := i + delta
	if i < 0 || i >= len(items) || j < 0 || j >= len(items) {
		return nil
	}
	items[i], items[j] = items[j], items[i]
	m := d.Exam.Manifest
	m.Items = items
	if err := d.write(ManifestFile, manifestNode(&m)); err != nil {
		return err
	}
	return d.Reload()
}

var numberedID = regexp.MustCompile(`^q(\d+)$`)

// NextQuestionID proposes an unused id following the q001, q002... scheme.
func (d *Draft) NextQuestionID() string {
	n := 0
	for id := range d.Exam.Questions {
		if m := numberedID.FindStringSubmatch(id); m != nil {
			v, _ := strconv.Atoi(m[1])
			n = max(n, v)
		}
	}
	for {
		n++
		id := fmt.Sprintf("q%03d", n)
		if d.Exam.Questions[id] == nil {
			return id
		}
	}
}

// QuestionOrder lists the question ids in the order the items name them,
// then any question no item references.
func (d *Draft) QuestionOrder() []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] && d.Exam.Questions[id] != nil {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, it := range d.Exam.Manifest.Items {
		if it.Pool != nil {
			for _, id := range it.Pool.From {
				add(id)
			}
		} else {
			add(it.Question)
		}
	}
	var rest []string
	for id := range d.Exam.Questions {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}

// keyOrder puts answers in question order, for keys new to the file.
func (d *Draft) keyOrder() []string { return d.QuestionOrder() }

func (d *Draft) referenced(id string) bool {
	for _, it := range d.Exam.Manifest.Items {
		if it.Question == id || (it.Pool != nil && slices.Contains(it.Pool.From, id)) {
			return true
		}
	}
	return false
}

func (d *Draft) abs(rel string) string { return filepath.Join(d.Dir, filepath.FromSlash(rel)) }

// write merges doc into the file rel (creating it if needed) and replaces
// the file atomically.
func (d *Draft) write(rel string, doc *yaml.Node) error {
	path := d.abs(rel)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out := doc
	if len(bytes.TrimSpace(old)) > 0 {
		var prev yaml.Node
		if err := yaml.Unmarshal(old, &prev); err != nil {
			return fmt.Errorf("%s: %w", rel, ErrBrokenFile)
		}
		if prev.Kind == yaml.DocumentNode && len(prev.Content) == 1 {
			merged := prev
			merged.Content = []*yaml.Node{merge(prev.Content[0], doc)}
			out = &merged
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	wrap := out
	if wrap.Kind != yaml.DocumentNode {
		wrap = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{out}}
	}
	if err := enc.Encode(wrap); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	data := keepBlankLines(old, buf.Bytes())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ax-*.yaml")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// merge returns next with the comments, order and styles of prev kept
// wherever the content did not change.
func merge(prev, next *yaml.Node) *yaml.Node {
	if prev == nil {
		return next
	}
	if prev.Kind != next.Kind {
		next.HeadComment, next.LineComment, next.FootComment = prev.HeadComment, prev.LineComment, prev.FootComment
		return next
	}
	switch next.Kind {
	case yaml.ScalarNode:
		if prev.Value == next.Value || sameDuration(prev.Value, next.Value) {
			return prev
		}
		out := *prev
		out.Value, out.Tag, out.Style = next.Value, next.Tag, next.Style
		if prev.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 && next.Style == 0 && !strings.Contains(next.Value, "\n") {
			out.Style = prev.Style // keep the author's quoting
		}
		return &out
	case yaml.SequenceNode:
		out := *prev
		out.Content = make([]*yaml.Node, len(next.Content))
		for i, n := range next.Content {
			var p *yaml.Node
			if i < len(prev.Content) {
				p = prev.Content[i]
			}
			out.Content[i] = merge(p, n)
		}
		return &out
	case yaml.MappingNode:
		old := map[string]int{}
		for i := 0; i+1 < len(prev.Content); i += 2 {
			old[prev.Content[i].Value] = i
		}
		want := map[string]*yaml.Node{}
		var order []string
		for i := 0; i+1 < len(next.Content); i += 2 {
			k := next.Content[i].Value
			want[k] = next.Content[i+1]
			order = append(order, k)
		}
		// Existing keys keep their place; new keys follow the key that
		// precedes them in next.
		var keys []string
		for i := 0; i+1 < len(prev.Content); i += 2 {
			if _, ok := want[prev.Content[i].Value]; ok {
				keys = append(keys, prev.Content[i].Value)
			}
		}
		for i, k := range order {
			if _, ok := old[k]; ok {
				continue
			}
			at := 0
			for j := i - 1; j >= 0; j-- {
				if p := slices.Index(keys, order[j]); p >= 0 {
					at = p + 1
					break
				}
			}
			keys = slices.Insert(keys, at, k)
		}
		out := *prev
		out.Content = nil
		for _, k := range keys {
			if i, ok := old[k]; ok {
				out.Content = append(out.Content, prev.Content[i], merge(prev.Content[i+1], want[k]))
			} else {
				out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, want[k])
			}
		}
		return &out
	}
	return next
}

func sameDuration(a, b string) bool {
	x, err1 := time.ParseDuration(a)
	y, err2 := time.ParseDuration(b)
	return err1 == nil && err2 == nil && x == y
}

// keepBlankLines puts back the blank lines that separated top-level
// sections in the original file; the YAML encoder drops them.
func keepBlankLines(old, out []byte) []byte {
	if len(old) == 0 {
		return out
	}
	oldLines := strings.Split(string(old), "\n")
	spaced := map[string]bool{}
	for i, l := range oldLines {
		k := topKey(l)
		if k == "" {
			continue
		}
		// Walk up over the comment block above the key.
		j := i - 1
		for j >= 0 && strings.HasPrefix(oldLines[j], "#") {
			j--
		}
		if j >= 0 && strings.TrimSpace(oldLines[j]) == "" {
			spaced[k] = true
		}
	}
	lines := strings.Split(string(out), "\n")
	var res []string
	for i, l := range lines {
		if k := topKey(l); k != "" && spaced[k] && i > 0 {
			// Insert above the key's comment block.
			at := len(res)
			for at > 0 && strings.HasPrefix(res[at-1], "#") {
				at--
			}
			if at > 0 && strings.TrimSpace(res[at-1]) != "" {
				res = slices.Insert(res, at, "")
			}
		}
		res = append(res, l)
	}
	return []byte(strings.Join(res, "\n"))
}

// topKey returns the key of a top-level "key: ..." line, or "".
func topKey(line string) string {
	if line == "" || line[0] == ' ' || line[0] == '#' || line[0] == '-' {
		return ""
	}
	k, _, ok := strings.Cut(line, ":")
	if !ok {
		return ""
	}
	return k
}

// ---------------- nodes ----------------

// mapping builds a mapping node from key/value pairs, skipping nil values.
func mapping(kv ...any) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i+1 < len(kv); i += 2 {
		v, ok := kv[i+1].(*yaml.Node)
		if !ok || v == nil {
			continue
		}
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: kv[i].(string)}, v)
	}
	return n
}

// str is a string scalar, or nil when empty. Multi-line text is a literal
// block so Markdown stays readable.
func str(s string) *yaml.Node {
	if s == "" {
		return nil
	}
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if strings.Contains(s, "\n") {
		n.Style = yaml.LiteralStyle
		if !strings.HasSuffix(n.Value, "\n") {
			n.Value += "\n"
		}
	}
	return n
}

func num(v float64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatFloat(v, 'f', -1, 64)}
}

func integer(v int) *yaml.Node {
	if v == 0 {
		return nil
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(v)}
}

func boolean(v bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatBool(v)}
}

func boolPtr(v *bool) *yaml.Node {
	if v == nil {
		return nil
	}
	return boolean(*v)
}

func numPtr(v *float64) *yaml.Node {
	if v == nil {
		return nil
	}
	return num(*v)
}

func duration(d Duration) *yaml.Node {
	if d == 0 {
		return nil
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: FormatDuration(d.D())}
}

// FormatDuration writes d the way people do: 45m, 1h30m, 90s → 1m30s.
func FormatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func seqOf(vals []string, flow bool) *yaml.Node {
	if len(vals) == 0 {
		return nil
	}
	n := &yaml.Node{Kind: yaml.SequenceNode}
	if flow {
		n.Style = yaml.FlowStyle
	}
	for _, v := range vals {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	return n
}

func nonEmpty(n *yaml.Node) *yaml.Node {
	if n == nil || len(n.Content) == 0 {
		return nil
	}
	return n
}

func manifestNode(m *Manifest) *yaml.Node {
	items := &yaml.Node{Kind: yaml.SequenceNode}
	for _, it := range m.Items {
		if it.Pool != nil {
			items.Content = append(items.Content, mapping("pool", mapping(
				"id", str(it.Pool.ID), "pick", integer(it.Pool.Pick), "from", seqOf(it.Pool.From, true))))
		} else {
			items.Content = append(items.Content, mapping("question", str(it.Question)))
		}
	}
	strikes := integer(m.Proctoring.StrikeLimit)
	return mapping(
		"format", str(m.Format),
		"id", str(m.ID),
		"title", str(m.Title),
		"language", str(m.Language),
		"duration", duration(m.Duration),
		"result_visibility", str(m.ResultVisibility),
		"instructions", str(m.Instructions),
		"order", nonEmpty(mapping("shuffle_questions", boolPtr(m.Order.ShuffleQuestions), "shuffle_options", boolPtr(m.Order.ShuffleOptions))),
		"proctoring", nonEmpty(mapping("profile", str(m.Proctoring.Profile), "fullscreen", str(m.Proctoring.Fullscreen),
			"devices", str(m.Proctoring.Devices), "reveal", str(m.Proctoring.Reveal), "strike_limit", strikes)),
		"items", items,
		"branding", nonEmpty(mapping("accent", str(m.Branding.Accent), "logo", str(m.Branding.Logo))),
	)
}

func questionNode(q *Question) *yaml.Node {
	opts := &yaml.Node{Kind: yaml.SequenceNode}
	for _, o := range q.Options {
		var pin *yaml.Node
		if o.Pin {
			pin = boolean(true)
		}
		opts.Content = append(opts.Content, mapping("id", str(o.ID), "text", str(o.Text), "pin", pin))
	}
	return mapping(
		"id", str(q.ID),
		"type", str(q.Type),
		"input", str(q.Input),
		"prompt", str(q.Prompt),
		"time_limit", duration(q.TimeLimit),
		"reveal", str(q.Reveal),
		"placeholder", str(q.Placeholder),
		"options", nonEmpty(opts),
	)
}

func answerNode(a Answer) *yaml.Node {
	var partial *yaml.Node
	if a.Partial {
		partial = boolean(true)
	}
	return mapping(
		"option", str(a.Option),
		"options", seqOf(a.Options, true),
		"partial", partial,
		"value", boolPtr(a.Value),
		"number", numPtr(a.Number),
		"tolerance", numPtr(a.Tolerance),
		"accept", seqOf(a.Accept, true),
		"normalize", seqOf(a.Normalize, true),
		"points", num(a.Points),
	)
}

// keyNode renders the answer key with answers in order (ids not in order
// come last, sorted).
func keyNode(k *AnswerKey, order []string) *yaml.Node {
	answers := &yaml.Node{Kind: yaml.MappingNode}
	seen := map[string]bool{}
	add := func(id string) {
		if a, ok := k.Answers[id]; ok && !seen[id] {
			seen[id] = true
			answers.Content = append(answers.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: id}, answerNode(a))
		}
	}
	for _, id := range order {
		add(id)
	}
	var rest []string
	for id := range k.Answers {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	slices.Sort(rest)
	for _, id := range rest {
		add(id)
	}
	return mapping("format", str(k.Format), "exam", str(k.Exam), "answers", answers)
}
