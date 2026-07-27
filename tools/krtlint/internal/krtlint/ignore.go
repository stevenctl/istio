// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package krtlint

import (
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// IgnoreDirective opts a single site out of one or more krt analyzers.
//
//	//krtlint:ignore                        -- silence every analyzer here
//	//krtlint:ignore krtequal               -- silence one
//	//krtlint:ignore krtequal,krtfetch      -- silence several
//
// Text after the directive is a free-form reason and is not interpreted, so the conventional
// form carries one:
//
//	//krtlint:ignore krtequal -- placeholder collection, never written to
//
// Keep the reason on the directive's own line. gofmt moves directive comments to the end of a
// doc comment block, which would strand any continuation lines above them; put a longer
// explanation in the prose before the directive instead.
//
// A directive applies to diagnostics reported anywhere in the comment group holding it, and on
// the line immediately after that group. That covers the two ways it is written: trailing a
// statement, or inside the doc comment of the declaration being excused, in which case the
// surrounding prose is part of the same group and the directive still reaches the declaration.
//
// Prefer +noKrtEquals on the field itself where that fits, since it survives the code moving
// around; reach for this when the diagnostic is not about a single field, or comes from an
// analyzer that has no marker of its own.
const IgnoreDirective = "//krtlint:ignore"

// ignoreSet records which analyzers are silenced on which lines of which files.
type ignoreSet struct {
	// lines maps file name and line to the analyzers silenced there. An empty set means
	// every analyzer.
	lines map[string]map[int]map[string]bool
}

func (s *ignoreSet) add(file string, line int, analyzers map[string]bool) {
	byLine, ok := s.lines[file]
	if !ok {
		byLine = map[int]map[string]bool{}
		s.lines[file] = byLine
	}
	existing, ok := byLine[line]
	if !ok {
		byLine[line] = analyzers
		return
	}
	// A line already silencing everything stays that way.
	if len(existing) == 0 {
		return
	}
	if len(analyzers) == 0 {
		byLine[line] = analyzers
		return
	}
	for name := range analyzers {
		existing[name] = true
	}
}

func (s *ignoreSet) suppresses(fset *token.FileSet, analyzer string, pos token.Pos) bool {
	if !pos.IsValid() {
		return false
	}
	p := fset.Position(pos)
	at, ok := s.lines[p.Filename][p.Line]
	if !ok {
		return false
	}
	return len(at) == 0 || at[analyzer]
}

// parseIgnores collects every ignore directive in the files under analysis.
//
// Scoping is per comment group rather than per comment line, so a directive stays attached to
// the declaration it documents however much prose surrounds it: a reason can run onto further
// lines, and the directive can sit anywhere in a doc comment.
func parseIgnores(pass *analysis.Pass) *ignoreSet {
	set := &ignoreSet{lines: map[string]map[int]map[string]bool{}}
	for _, file := range pass.Files {
		for _, group := range file.Comments {
			analyzers, found := map[string]bool{}, false
			for _, comment := range group.List {
				named, ok := parseIgnoreDirective(comment.Text)
				if !ok {
					continue
				}
				found = true
				// Two directives in one group widen to the union, and one naming nothing
				// widens to everything.
				if len(named) == 0 {
					analyzers = map[string]bool{}
					break
				}
				for name := range named {
					analyzers[name] = true
				}
			}
			if !found {
				continue
			}
			start := pass.Fset.Position(group.Pos())
			end := pass.Fset.Position(group.End())
			// The group's own lines, plus the line it sits above.
			for line := start.Line; line <= end.Line+1; line++ {
				set.add(start.Filename, line, analyzers)
			}
		}
	}
	return set
}

// parseIgnoreDirective reports whether text is an ignore directive, and which analyzers it
// names. An empty set means every analyzer.
func parseIgnoreDirective(text string) (map[string]bool, bool) {
	body, ok := strings.CutPrefix(strings.TrimSpace(text), IgnoreDirective)
	if !ok {
		return nil, false
	}
	// Guard against matching a longer directive that merely shares the prefix.
	if body != "" && !strings.HasPrefix(body, " ") && !strings.HasPrefix(body, "\t") {
		return nil, false
	}
	// Everything from the reason separator on is prose.
	if reason := strings.Index(body, "--"); reason >= 0 {
		body = body[:reason]
	}
	names := map[string]bool{}
	for _, name := range strings.Split(body, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names[name] = true
		}
	}
	return names, true
}

// withIgnores wraps an analyzer's Run so that diagnostics landing on an ignored line are
// dropped. The analyzer is modified in place, so identity is preserved for anything that
// depends on it.
//
// The wrapper hands the inner Run a shallow copy of the pass carrying a filtering Report.
// Everything else, including ResultOf and the fact set, is shared with the original pass, so
// the analyzer cannot tell the difference.
func withIgnores(a *analysis.Analyzer) *analysis.Analyzer {
	run := a.Run
	a.Run = func(pass *analysis.Pass) (any, error) {
		ignores := parseIgnores(pass)
		filtered := *pass
		filtered.Report = func(d analysis.Diagnostic) {
			if ignores.suppresses(pass.Fset, a.Name, d.Pos) {
				return
			}
			pass.Report(d)
		}
		return run(&filtered)
	}
	return a
}
