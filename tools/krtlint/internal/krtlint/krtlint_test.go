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
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestAnalyzers runs each analyzer over its own testdata package. It drives them through
// Analyzers() rather than the bare vars so that the //nokrtlint wrapping, which is what
// ships, is the thing under test.
func TestAnalyzers(t *testing.T) {
	pkg := map[string]string{
		KeyAnalyzer.Name:          "key",
		EqualAnalyzer.Name:        "equal",
		EqualsFieldsAnalyzer.Name: "equalsfields",
		FetchAnalyzer.Name:        "fetch",
		FilterAnalyzer.Name:       "filter",
	}
	for _, a := range Analyzers() {
		dir, ok := pkg[a.Name]
		if !ok {
			t.Fatalf("analyzer %s has no testdata package", a.Name)
		}
		t.Run(a.Name, func(t *testing.T) {
			analysistest.Run(t, analysistest.TestData(), a, dir)
		})
	}
}
