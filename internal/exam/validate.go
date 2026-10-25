package exam

import (
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	identRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	colorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	// Inline Markdown images: ![alt](target "title")
	imageRe = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
)

var (
	languages     = []string{"es", "en"}
	visibilities  = []string{"none", "score", "review"}
	profiles      = []string{"off", "standard", "strict"}
	reveals       = []string{"always", "hover"}
	fullscreens   = []string{"off", "suggested", "required"}
	devices       = []string{"any", "desktop"}
	inputs        = []string{"number", "text"}
	normalizers   = []string{"trim", "casefold", "collapse_spaces", "strip_accents"}
	questionTypes = []string{TypeSingle, TypeMultiple, TypeTrueFalse, TypeShort}
	reservedTypes = []string{"essay", "parametric"}
)

// ValidID reports whether s is a valid identifier for exams, questions,
// options and pools.
func ValidID(s string) bool { return len(s) <= MaxIdentifierSize && identRe.MatchString(s) }

func (l *loader) validate() {
	if l.manifestOK {
		l.validateManifest()
	}
	for _, id := range l.questionIDs() {
		if !l.broken[id] {
			l.validateQuestion(l.ex.Questions[id])
		}
	}
	if l.keyOK {
		l.validateKey()
	}
	if l.manifestOK && l.keyOK {
		l.validatePoolPoints()
	}
}

func (l *loader) questionIDs() []string {
	ids := make([]string, 0, len(l.ex.Questions))
	for id := range l.ex.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (l *loader) checkID(file string, p fieldPath, id string) {
	if !ValidID(id) {
		l.errorf(file, p, "invalid id %q: use lowercase letters, digits, - and _ (max %d characters)", id, MaxIdentifierSize)
	}
}

func (l *loader) checkEnum(file string, p fieldPath, v string, allowed []string) {
	if v != "" && !slices.Contains(allowed, v) {
		l.errorf(file, p, "invalid value %q; use one of: %s", v, strings.Join(allowed, ", "))
	}
}

func (l *loader) validateManifest() {
	m := &l.ex.Manifest
	f := ManifestFile

	switch m.Format {
	case FormatV1:
	case "":
		l.errorf(f, fieldPath{"format"}, "required; use %q", FormatV1)
	default:
		l.errorf(f, fieldPath{"format"}, "unsupported format %q; this version of ax reads %q", m.Format, FormatV1)
	}

	if m.ID == "" {
		l.errorf(f, fieldPath{"id"}, "required")
	} else {
		l.checkID(f, fieldPath{"id"}, m.ID)
		if base := filepath.Base(filepath.Clean(l.dir)); ValidID(m.ID) && Slug(base) != m.ID {
			l.warnf(f, fieldPath{"id"}, "id %q does not match the folder name %q", m.ID, base)
		}
	}

	if strings.TrimSpace(m.Title) == "" {
		l.errorf(f, fieldPath{"title"}, "required")
	} else if utf8.RuneCountInString(m.Title) > 200 {
		l.errorf(f, fieldPath{"title"}, "longer than 200 characters")
	}

	l.checkEnum(f, fieldPath{"language"}, m.Language, languages)
	l.checkEnum(f, fieldPath{"result_visibility"}, m.ResultVisibility, visibilities)
	l.checkEnum(f, fieldPath{"proctoring", "profile"}, m.Proctoring.Profile, profiles)
	l.checkEnum(f, fieldPath{"proctoring", "fullscreen"}, m.Proctoring.Fullscreen, fullscreens)
	l.checkEnum(f, fieldPath{"proctoring", "devices"}, m.Proctoring.Devices, devices)
	l.checkEnum(f, fieldPath{"proctoring", "reveal"}, m.Proctoring.Reveal, reveals)
	if n := m.Proctoring.StrikeLimit; n < 0 || n > 10 {
		l.errorf(f, fieldPath{"proctoring", "strike_limit"}, "must be between 1 and 10")
	}

	switch d := m.Duration.D(); {
	case d == 0:
		l.errorf(f, fieldPath{"duration"}, "required (e.g. 45m)")
	case d < 0:
		l.errorf(f, fieldPath{"duration"}, "must be positive")
	case d > MaxDuration:
		l.errorf(f, fieldPath{"duration"}, "longer than %s", MaxDuration)
	}

	l.checkMarkdown(f, fieldPath{"instructions"}, m.Instructions)

	if m.Branding.Accent != "" && !colorRe.MatchString(m.Branding.Accent) {
		l.errorf(f, fieldPath{"branding", "accent"}, "invalid color %q; use a hex color such as #4f46e5", m.Branding.Accent)
	}
	if m.Branding.Logo != "" {
		l.checkAssetRef(f, fieldPath{"branding", "logo"}, m.Branding.Logo)
	}

	l.validateItems()
}

func (l *loader) validateItems() {
	m := &l.ex.Manifest
	f := ManifestFile
	if len(m.Items) == 0 {
		l.errorf(f, fieldPath{"items"}, "required; list at least one question")
		return
	}

	used := map[string]string{} // question id -> where it is used
	use := func(p fieldPath, id string) {
		if id == "" {
			l.errorf(f, p, "empty question id")
			return
		}
		if _, ok := l.ex.Questions[id]; !ok {
			l.errorf(f, p, "unknown question %q (expected %s/%s.yaml)", id, QuestionsDir, id)
			return
		}
		if where, dup := used[id]; dup {
			l.errorf(f, p, "question %q is already used at %s", id, where)
			return
		}
		used[id] = p.String()
	}

	pools := map[string]bool{}
	for i, it := range m.Items {
		p := fieldPath{"items", i}
		switch {
		case it.Question != "" && it.Pool != nil:
			l.errorf(f, p, "an item is either a question or a pool, not both")
		case it.Question == "" && it.Pool == nil:
			l.errorf(f, p, "set either question or pool")
		case it.Question != "":
			use(p.with("question"), it.Question)
		default:
			pp := p.with("pool")
			pool := it.Pool
			if pool.ID == "" {
				l.errorf(f, pp.with("id"), "required")
			} else {
				l.checkID(f, pp.with("id"), pool.ID)
				if pools[pool.ID] {
					l.errorf(f, pp.with("id"), "duplicate pool id %q", pool.ID)
				}
				pools[pool.ID] = true
			}
			for j, id := range pool.From {
				use(pp.with("from", j), id)
			}
			switch {
			case len(pool.From) < 2:
				l.errorf(f, pp.with("from"), "a pool needs at least 2 questions")
			case pool.Pick < 1 || pool.Pick >= len(pool.From):
				if limit := len(pool.From) - 1; limit == 1 {
					l.errorf(f, pp.with("pick"), "must be 1 (a pool must leave at least one question out)")
				} else {
					l.errorf(f, pp.with("pick"), "must be between 1 and %d (a pool must leave at least one question out)", limit)
				}
			}
		}
	}

	for _, id := range l.questionIDs() {
		if _, ok := used[id]; !ok {
			l.warnf(l.ex.Questions[id].File, nil, "question is not used by any item in %s", ManifestFile)
		}
	}
}

func (l *loader) validateQuestion(q *Question) {
	f := q.File
	switch {
	case q.Type == "":
		l.errorf(f, fieldPath{"type"}, "required; use one of: %s", strings.Join(questionTypes, ", "))
		return
	case slices.Contains(reservedTypes, q.Type):
		l.errorf(f, fieldPath{"type"}, "type %q is reserved for a future version of ax", q.Type)
		return
	case !slices.Contains(questionTypes, q.Type):
		l.errorf(f, fieldPath{"type"}, "invalid value %q; use one of: %s", q.Type, strings.Join(questionTypes, ", "))
		return
	}

	if strings.TrimSpace(q.Prompt) == "" {
		l.errorf(f, fieldPath{"prompt"}, "required")
	}
	l.checkMarkdown(f, fieldPath{"prompt"}, q.Prompt)
	l.checkEnum(f, fieldPath{"reveal"}, q.Reveal, reveals)

	if tl := q.TimeLimit.D(); tl != 0 {
		switch total := l.ex.Manifest.Duration.D(); {
		case tl < MinTimeLimit:
			l.errorf(f, fieldPath{"time_limit"}, "must be at least %s", MinTimeLimit)
		case l.manifestOK && total > 0 && tl > total:
			l.errorf(f, fieldPath{"time_limit"}, "longer than the exam duration (%s)", total)
		}
	}

	choice := q.Type == TypeSingle || q.Type == TypeMultiple
	if choice {
		l.validateOptions(q)
	} else if len(q.Options) > 0 {
		l.errorf(f, fieldPath{"options"}, "only single and multiple questions have options")
	}

	if q.Type == TypeShort {
		if q.Input == "" {
			l.errorf(f, fieldPath{"input"}, "required for short questions; use number or text")
		}
		l.checkEnum(f, fieldPath{"input"}, q.Input, inputs)
		if utf8.RuneCountInString(q.Placeholder) > 100 {
			l.errorf(f, fieldPath{"placeholder"}, "longer than 100 characters")
		}
	} else {
		if q.Input != "" {
			l.errorf(f, fieldPath{"input"}, "only short questions have an input type")
		}
		if q.Placeholder != "" {
			l.errorf(f, fieldPath{"placeholder"}, "only short questions have a placeholder")
		}
	}
}

func (l *loader) validateOptions(q *Question) {
	f := q.File
	switch n := len(q.Options); {
	case n < MinOptions:
		l.errorf(f, fieldPath{"options"}, "%s questions need at least %d options", q.Type, MinOptions)
	case n > MaxOptions:
		l.errorf(f, fieldPath{"options"}, "too many options (%d); the limit is %d", n, MaxOptions)
	}
	seen := map[string]int{}
	for i, o := range q.Options {
		p := fieldPath{"options", i}
		if o.ID == "" {
			l.errorf(f, p.with("id"), "required")
		} else {
			l.checkID(f, p.with("id"), o.ID)
			if j, dup := seen[o.ID]; dup {
				l.errorf(f, p.with("id"), "duplicate option id %q (also options[%d])", o.ID, j)
			}
			seen[o.ID] = i
		}
		if strings.TrimSpace(o.Text) == "" {
			l.errorf(f, p.with("text"), "required")
		}
		l.checkMarkdown(f, p.with("text"), o.Text)
	}
}

func (l *loader) validateKey() {
	k := &l.ex.Key
	f := AnswerKeyFile

	switch k.Format {
	case FormatV1:
	case "":
		l.errorf(f, fieldPath{"format"}, "required; use %q", FormatV1)
	default:
		l.errorf(f, fieldPath{"format"}, "unsupported format %q; this version of ax reads %q", k.Format, FormatV1)
	}
	switch {
	case k.Exam == "":
		l.errorf(f, fieldPath{"exam"}, "required; use the manifest id")
	case l.manifestOK && l.ex.Manifest.ID != "" && k.Exam != l.ex.Manifest.ID:
		l.errorf(f, fieldPath{"exam"}, "%q does not match the manifest id %q", k.Exam, l.ex.Manifest.ID)
	}

	for _, id := range l.questionIDs() {
		if _, ok := k.Answers[id]; !ok {
			l.errorf(f, fieldPath{"answers"}, "missing entry for question %q", id)
		}
	}

	ids := make([]string, 0, len(k.Answers))
	for id := range k.Answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		q, ok := l.ex.Questions[id]
		p := fieldPath{"answers", id}
		if !ok {
			l.errorf(f, p, "unknown question %q", id)
			continue
		}
		a := k.Answers[id]
		switch {
		case a.Points <= 0:
			l.errorf(f, p.with("points"), "required; must be greater than 0")
		case a.Points > MaxPoints:
			l.errorf(f, p.with("points"), "must be at most %d", MaxPoints)
		}
		if !l.broken[id] {
			l.validateAnswer(q, a, p)
		}
	}
}

// answerFields lists the type-specific fields that are set in a.
func answerFields(a Answer) []string {
	var set []string
	add := func(name string, ok bool) {
		if ok {
			set = append(set, name)
		}
	}
	add("option", a.Option != "")
	add("options", a.Options != nil)
	add("partial", a.Partial)
	add("value", a.Value != nil)
	add("number", a.Number != nil)
	add("tolerance", a.Tolerance != nil)
	add("accept", a.Accept != nil)
	add("normalize", a.Normalize != nil)
	return set
}

func (l *loader) validateAnswer(q *Question, a Answer, p fieldPath) {
	f := AnswerKeyFile
	kind := q.Type
	var allowed []string
	switch q.Type {
	case TypeSingle:
		allowed = []string{"option"}
	case TypeMultiple:
		allowed = []string{"options", "partial"}
	case TypeTrueFalse:
		allowed = []string{"value"}
	case TypeShort:
		switch q.Input {
		case "number":
			kind, allowed = "short number", []string{"number", "tolerance"}
		case "text":
			kind, allowed = "short text", []string{"accept", "normalize"}
		default:
			return // the question itself is invalid
		}
	default:
		return
	}
	for _, name := range answerFields(a) {
		if !slices.Contains(allowed, name) {
			l.errorf(f, p.with(name), "does not apply to %s questions (use %s)", kind, strings.Join(allowed, ", "))
		}
	}

	optionExists := func(id string) bool {
		return slices.ContainsFunc(q.Options, func(o Option) bool { return o.ID == id })
	}

	switch kind {
	case TypeSingle:
		if a.Option == "" {
			l.errorf(f, p.with("option"), "required: the id of the correct option")
		} else if !optionExists(a.Option) {
			l.errorf(f, p.with("option"), "unknown option %q in %s", a.Option, q.File)
		}
	case TypeMultiple:
		if len(a.Options) == 0 {
			l.errorf(f, p.with("options"), "required: the ids of the correct options")
		}
		seen := map[string]bool{}
		for i, id := range a.Options {
			switch {
			case seen[id]:
				l.errorf(f, p.with("options", i), "duplicate option %q", id)
			case !optionExists(id):
				l.errorf(f, p.with("options", i), "unknown option %q in %s", id, q.File)
			}
			seen[id] = true
		}
	case TypeTrueFalse:
		if a.Value == nil {
			l.errorf(f, p.with("value"), "required: true or false")
		}
	case "short number":
		if a.Number == nil {
			l.errorf(f, p.with("number"), "required: the correct number")
		}
		if a.Tolerance != nil && *a.Tolerance < 0 {
			l.errorf(f, p.with("tolerance"), "must not be negative")
		}
	case "short text":
		if len(a.Accept) == 0 {
			l.errorf(f, p.with("accept"), "required: list the accepted answers")
		}
		for i, s := range a.Accept {
			if strings.TrimSpace(s) == "" {
				l.errorf(f, p.with("accept", i), "empty accepted answer")
			}
		}
		seen := map[string]bool{}
		for i, n := range a.Normalize {
			switch {
			case !slices.Contains(normalizers, n):
				l.errorf(f, p.with("normalize", i), "invalid value %q; use one of: %s", n, strings.Join(normalizers, ", "))
			case seen[n]:
				l.errorf(f, p.with("normalize", i), "duplicate step %q", n)
			}
			seen[n] = true
		}
	}
}

// validatePoolPoints requires every question of a pool to award the same
// points, so attempts drawing different questions stay comparable.
func (l *loader) validatePoolPoints() {
	for i, it := range l.ex.Manifest.Items {
		if it.Pool == nil {
			continue
		}
		var first string
		var parts []string
		mismatch := false
		for _, id := range it.Pool.From {
			a, ok := l.ex.Key.Answers[id]
			if !ok || a.Points <= 0 {
				continue
			}
			parts = append(parts, id+": "+FormatPoints(a.Points))
			if first == "" {
				first = id
			} else if a.Points != l.ex.Key.Answers[first].Points {
				mismatch = true
			}
		}
		if mismatch {
			l.errorf(ManifestFile, fieldPath{"items", i, "pool", "from"},
				"all questions in a pool must award the same points (%s)", strings.Join(parts, ", "))
		}
	}
}

func (l *loader) checkMarkdown(file string, p fieldPath, text string) {
	if len(text) > MaxMarkdownBytes {
		l.errorf(file, p, "text is larger than %d KiB", MaxMarkdownBytes>>10)
		return
	}
	for _, m := range imageRe.FindAllStringSubmatch(text, -1) {
		l.checkAssetRef(file, p, m[1])
	}
}

// checkAssetRef requires ref to name an existing file under assets/.
func (l *loader) checkAssetRef(file string, p fieldPath, ref string) {
	if strings.Contains(ref, "://") || strings.HasPrefix(ref, "//") || strings.HasPrefix(ref, "data:") {
		l.errorf(file, p, "external image %q is not allowed; copy the file into %s/ and reference it as %s/<name>", ref, AssetsDir, AssetsDir)
		return
	}
	clean := path.Clean(strings.ReplaceAll(ref, `\`, "/"))
	if !strings.HasPrefix(clean, AssetsDir+"/") {
		l.errorf(file, p, "%q must point to a file under %s/", ref, AssetsDir)
		return
	}
	if !l.assets[clean] {
		l.errorf(file, p, "asset %q not found", clean)
	}
}
