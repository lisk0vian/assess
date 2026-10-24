package exam

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// fieldPath is a field location inside a YAML document: string keys and int indexes.
type fieldPath []any

func (p fieldPath) String() string {
	var b strings.Builder
	for _, e := range p {
		switch v := e.(type) {
		case int:
			fmt.Fprintf(&b, "[%d]", v)
		case string:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(v)
		}
	}
	return b.String()
}

// with returns a copy of p extended with elems.
func (p fieldPath) with(elems ...any) fieldPath {
	out := make(fieldPath, 0, len(p)+len(elems))
	return append(append(out, p...), elems...)
}

// lineOf returns the line of the deepest node found along p, or 0.
// For a mapping key the key's line is used, which is where editors point.
func lineOf(doc *yaml.Node, p fieldPath) int {
	if doc == nil {
		return 0
	}
	n := doc
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	line := n.Line
	for _, e := range p {
		switch v := e.(type) {
		case string:
			if n.Kind != yaml.MappingNode {
				return line
			}
			found := false
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == v {
					line = n.Content[i].Line
					n = n.Content[i+1]
					found = true
					break
				}
			}
			if !found {
				return line
			}
		case int:
			if n.Kind != yaml.SequenceNode || v < 0 || v >= len(n.Content) {
				return line
			}
			n = n.Content[v]
			line = n.Line
		}
	}
	return line
}

// decodeStrict parses data into v, rejecting unknown fields, and also returns
// the node tree used to locate later semantic problems. Decoding problems are
// returned as messages with line numbers.
func decodeStrict(data []byte, v any) (*yaml.Node, []lineMsg) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, yamlErrors(err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, []lineMsg{{0, "file is empty"}}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return &doc, yamlErrors(err)
	}
	return &doc, nil
}

type lineMsg struct {
	line int
	msg  string
}

var lineRe = regexp.MustCompile(`^(?:yaml: )?line (\d+): (.*)$`)

// yamlErrors turns yaml library errors into friendly, line-tagged messages.
func yamlErrors(err error) []lineMsg {
	var raw []string
	var te *yaml.TypeError
	if errors.As(err, &te) {
		raw = te.Errors
	} else {
		raw = []string{err.Error()}
	}
	out := make([]lineMsg, 0, len(raw))
	for _, r := range raw {
		lm := lineMsg{msg: r}
		if m := lineRe.FindStringSubmatch(r); m != nil {
			lm.line, _ = strconv.Atoi(m[1])
			lm.msg = m[2]
		}
		lm.msg = friendly(strings.TrimPrefix(lm.msg, "yaml: "))
		out = append(out, lm)
	}
	return out
}

var (
	unknownFieldRe = regexp.MustCompile(`^field (\S+) not found in type \S+$`)
	unmarshalRe    = regexp.MustCompile("^cannot unmarshal !!(\\w+) `?(.*?)`? into (\\S+)$")
)

func friendly(msg string) string {
	if m := unknownFieldRe.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("unknown field %q", m[1])
	}
	if m := unmarshalRe.FindStringSubmatch(msg); m != nil {
		got := m[2]
		switch m[1] {
		case "map":
			got = "a mapping"
		case "seq":
			got = "a list"
		default:
			got = strconv.Quote(got)
		}
		return fmt.Sprintf("expected %s, got %s", goTypeName(m[3]), got)
	}
	return msg
}

func goTypeName(t string) string {
	t = strings.TrimPrefix(t, "*")
	switch {
	case strings.HasPrefix(t, "[]"):
		return "a list"
	case strings.HasPrefix(t, "map["), strings.HasPrefix(t, "exam."):
		return "a mapping"
	case strings.HasPrefix(t, "int"), strings.HasPrefix(t, "uint"):
		return "an integer"
	case strings.HasPrefix(t, "float"):
		return "a number"
	case t == "bool":
		return "true or false"
	case t == "string":
		return "text"
	}
	return t
}
