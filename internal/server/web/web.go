// Package web serves the student single-page app embedded in the binary.
// The Vite build of web/ writes into dist/.
package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/lisk0vian/assess/internal/i18n"
)

//go:embed all:dist
var dist embed.FS

func init() {
	// Go's mime table does not know .webmanifest; browsers need the JSON type.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// Strings returns the student page's texts in lang, with English filling
// the gaps (the web section of the catalogs).
func Strings(lang string) map[string]string { return i18n.Section(lang, "web") }

// StringsHandler serves Strings(lang) as JSON, for the Vite dev server,
// which does not get them injected into the page.
func StringsHandler(lang string) http.Handler {
	data, _ := json.Marshal(Strings(lang))
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
}

var htmlLang = regexp.MustCompile(`<html lang="[^"]*">`)

// page is index.html with the texts of lang injected, so the page renders
// in the exam's language without another request.
func page(root fs.FS, lang string) []byte {
	html, err := fs.ReadFile(root, "index.html")
	if err != nil {
		panic(err)
	}
	data, _ := json.Marshal(Strings(lang)) // escapes <, > and &: safe inside <script>
	script := `<script id="ax-i18n" type="application/json">` + string(data) + `</script>`
	html = htmlLang.ReplaceAll(html, []byte(`<html lang="`+lang+`">`))
	return bytes.Replace(html, []byte("</head>"), []byte(script+"\n  </head>"), 1)
}

// Handler serves dist/ with SPA fallback: unknown paths get index.html so
// client-side routes work after a reload. lang is the exam's language.
func Handler(lang string) http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	index := page(root, lang)
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "." {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			p = "index.html"
		}
		if p == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(index)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			// Vite file names carry a content hash.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
