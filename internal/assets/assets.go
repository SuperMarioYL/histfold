// Package assets holds static templates embedded into the histfold binary.
package assets

import _ "embed"

// Timeline is the HTML template for the self-contained fold timeline: one
// collapsed card per changeset, expandable to its raw commits, no external
// requests.
//
//go:embed timeline.html
var Timeline string
