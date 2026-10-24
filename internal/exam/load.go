package exam

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Load reads and validates the exam folder at dir.
//
// The error is non-nil only when dir itself cannot be read. Content problems
// are returned as Problems; the Exam is returned even when it has errors so
// callers can inspect what was loaded.
func Load(dir string) (*Exam, Problems, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a folder", dir)
	}

	l := &loader{
		dir:       dir,
		ex:        &Exam{Dir: dir, Questions: map[string]*Question{}},
		docs:      map[string]*yaml.Node{},
		decodeErr: map[string]map[int]bool{},
		broken:    map[string]bool{},
		assets:    map[string]bool{},
	}
	l.checkRoot()
	l.manifestOK = l.readYAML(ManifestFile, &l.ex.Manifest)
	l.readQuestions()
	l.keyOK = l.readYAML(AnswerKeyFile, &l.ex.Key)
	l.readAssets()

	l.validate()

	l.probs.Sort()
	return l.ex, l.probs, nil
}

type loader struct {
	dir   string
	ex    *Exam
	docs  map[string]*yaml.Node // parsed documents by relative file, for line lookup
	probs Problems

	decodeErr map[string]map[int]bool // lines with decoding errors, by file

	manifestOK bool            // manifest parsed (possibly with field errors)
	keyOK      bool            // answer key parsed (possibly with field errors)
	broken     map[string]bool // question ids whose file could not be parsed
	assets     map[string]bool // slash paths of files under assets/
}

func (l *loader) add(sev Severity, file string, p fieldPath, format string, args ...any) {
	line := lineOf(l.docs[file], p)
	if sev == SeverityError && line > 0 && l.decodeErr[file][line] {
		return // the decoding error on this line already explains the problem
	}
	l.probs = append(l.probs, Problem{
		Severity: sev,
		File:     file,
		Line:     line,
		Field:    p.String(),
		Message:  fmt.Sprintf(format, args...),
	})
}

func (l *loader) errorf(file string, p fieldPath, format string, args ...any) {
	l.add(SeverityError, file, p, format, args...)
}

func (l *loader) warnf(file string, p fieldPath, format string, args ...any) {
	l.add(SeverityWarning, file, p, format, args...)
}

func (l *loader) abs(rel string) string { return filepath.Join(l.dir, filepath.FromSlash(rel)) }

// readYAML strictly decodes one file. It reports whether the file was parsed;
// field-level errors are reported but still leave the rest of v decoded.
func (l *loader) readYAML(rel string, v any) bool {
	info, err := os.Lstat(l.abs(rel))
	switch {
	case os.IsNotExist(err):
		l.errorf(rel, nil, "file not found")
		return false
	case err != nil:
		l.errorf(rel, nil, "cannot read file: %v", err)
		return false
	case info.Mode()&fs.ModeSymlink != 0:
		l.errorf(rel, nil, "symbolic links are not allowed")
		return false
	case !info.Mode().IsRegular():
		l.errorf(rel, nil, "not a regular file")
		return false
	case info.Size() > MaxYAMLFileBytes:
		l.errorf(rel, nil, "file is larger than %d KiB", MaxYAMLFileBytes>>10)
		return false
	}
	data, err := os.ReadFile(l.abs(rel))
	if err != nil {
		l.errorf(rel, nil, "cannot read file: %v", err)
		return false
	}
	doc, msgs := decodeStrict(data, v)
	if doc != nil {
		l.docs[rel] = doc
	}
	lines := map[int]bool{}
	for _, m := range msgs {
		l.probs = append(l.probs, Problem{Severity: SeverityError, File: rel, Line: m.line, Message: m.msg})
		lines[m.line] = true
	}
	l.decodeErr[rel] = lines
	return doc != nil
}

// checkRoot warns about unexpected entries next to the manifest.
func (l *loader) checkRoot() {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		l.errorf(".", nil, "cannot list folder: %v", err)
		return
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case name == ManifestFile, name == QuestionsDir, name == PrivateDir, name == AssetsDir:
		case strings.HasPrefix(name, "."), strings.HasPrefix(strings.ToUpper(name), "README"):
		default:
			l.warnf(name, nil, "unexpected file or folder; it is ignored")
		}
	}
	if entries, err := os.ReadDir(l.abs(PrivateDir)); err == nil {
		for _, e := range entries {
			if rel := PrivateDir + "/" + e.Name(); rel != AnswerKeyFile && !strings.HasPrefix(e.Name(), ".") {
				l.warnf(rel, nil, "unexpected file or folder; it is ignored")
			}
		}
	}
}

func (l *loader) readQuestions() {
	entries, err := os.ReadDir(l.abs(QuestionsDir))
	if os.IsNotExist(err) {
		l.errorf(QuestionsDir+"/", nil, "folder not found")
		return
	}
	if err != nil {
		l.errorf(QuestionsDir+"/", nil, "cannot list folder: %v", err)
		return
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		rel := QuestionsDir + "/" + name
		switch {
		case strings.HasPrefix(name, "."):
		case e.IsDir():
			l.warnf(rel, nil, "subfolders are not supported; it is ignored")
		case strings.HasSuffix(name, ".yml"):
			l.errorf(rel, nil, "use the .yaml extension")
		case strings.HasSuffix(name, ".yaml"):
			files = append(files, name)
		default:
			l.warnf(rel, nil, "not a .yaml file; it is ignored")
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		l.errorf(QuestionsDir+"/", nil, "no questions found")
		return
	}
	if len(files) > MaxQuestions {
		l.errorf(QuestionsDir+"/", nil, "too many questions (%d); the limit is %d", len(files), MaxQuestions)
		return
	}
	for _, name := range files {
		rel := QuestionsDir + "/" + name
		q := &Question{File: rel}
		ok := l.readYAML(rel, q)
		stem := strings.TrimSuffix(name, ".yaml")
		if q.ID == "" {
			if ok {
				l.errorf(rel, fieldPath{"id"}, "required")
			}
			q.ID = stem // keep references to this file resolvable
		} else if q.ID != stem {
			l.errorf(rel, fieldPath{"id"}, "id %q must match the file name (%s.yaml)", q.ID, q.ID)
		}
		if prev, dup := l.ex.Questions[q.ID]; dup {
			l.errorf(rel, fieldPath{"id"}, "duplicate question id %q (also in %s)", q.ID, prev.File)
			continue
		}
		l.ex.Questions[q.ID] = q
		if !ok {
			l.broken[q.ID] = true
		}
	}
}

var servedAssetExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true,
}

func (l *loader) readAssets() {
	root := l.abs(AssetsDir)
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return
	}
	var total int64
	count := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relOS, _ := filepath.Rel(l.dir, p)
		rel := filepath.ToSlash(relOS)
		if d.Type()&fs.ModeSymlink != 0 {
			l.errorf(rel, nil, "symbolic links are not allowed")
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		count++
		total += info.Size()
		if !servedAssetExt[strings.ToLower(filepath.Ext(d.Name()))] {
			l.warnf(rel, nil, "file type is not served to students (allowed: png, jpg, gif, webp, svg)")
			return nil
		}
		l.assets[rel] = true
		l.ex.Assets = append(l.ex.Assets, rel)
		return nil
	})
	if err != nil {
		l.errorf(AssetsDir+"/", nil, "cannot read folder: %v", err)
	}
	if count > MaxAssetFiles {
		l.errorf(AssetsDir+"/", nil, "too many files (%d); the limit is %d", count, MaxAssetFiles)
	}
	if total > MaxAssetsBytes {
		l.errorf(AssetsDir+"/", nil, "assets total %.1f MiB; the limit is %d MiB", float64(total)/(1<<20), MaxAssetsBytes>>20)
	}
	sort.Strings(l.ex.Assets)
}
