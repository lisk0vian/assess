package exam

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Published is an immutable snapshot of a valid exam. Sessions run against a
// Published version, so editing the YAML never affects a running session.
type Published struct {
	Hash   string            // hex SHA-256 over every file of the exam
	Exam   *Exam             // parsed content; Dir is cleared
	Assets map[string][]byte // asset bytes by slash path
}

// ErrInvalidExam is returned when publishing an exam with errors.
var ErrInvalidExam = errors.New("exam has validation errors")

// Publish loads, validates and snapshots the exam folder at dir.
func Publish(dir string) (*Published, Problems, error) {
	ex, probs, err := Load(dir)
	if err != nil {
		return nil, nil, err
	}
	if probs.HasErrors() {
		return nil, probs, ErrInvalidExam
	}

	files := []string{ManifestFile, AnswerKeyFile}
	for _, q := range ex.Questions {
		files = append(files, q.File)
	}
	files = append(files, ex.Assets...)
	sort.Strings(files)

	pub := &Published{Assets: map[string][]byte{}}
	isAsset := assetSet(ex.Assets)
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", FormatV1)
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, probs, fmt.Errorf("publish: %w", err)
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%x\n", rel, sum)
		if _, ok := isAsset[rel]; ok {
			pub.Assets[rel] = data
		}
	}
	pub.Hash = hex.EncodeToString(h.Sum(nil))

	snapshot := *ex
	snapshot.Dir = ""
	pub.Exam = &snapshot
	return pub, probs, nil
}

func assetSet(assets []string) map[string]struct{} {
	m := make(map[string]struct{}, len(assets))
	for _, a := range assets {
		m[a] = struct{}{}
	}
	return m
}
