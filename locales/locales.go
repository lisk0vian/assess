// Package locales embeds the built-in translation catalogs, one YAML file
// per language. The format is described in internal/i18n; README.md in
// this folder explains how to add a language.
package locales

import "embed"

// FS holds the catalogs (en.yaml, es.yaml, ...).
//
//go:embed *.yaml
var FS embed.FS
