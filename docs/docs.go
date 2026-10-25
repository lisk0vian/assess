// Package docs embeds the documentation that ax prints, so it is available
// wherever the binary is, offline.
package docs

import _ "embed"

// Format is the reference of the exam format.
//
//go:embed format.md
var Format string

// Authoring is the guide to writing an exam as files, for AI assistants.
//
//go:embed authoring.md
var Authoring string
