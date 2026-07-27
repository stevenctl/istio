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

// krtlint checks for correct usage of the krt library. Run it like any other go vet
// tool, for example `go run ./tools/krtlint ./pilot/...`.
package main

import (
	"flag"

	"golang.org/x/tools/go/analysis/multichecker"

	"istio.io/istio/tools/krtlint/internal/krtlint"
)

func main() {
	// multichecker parses flag.CommandLine, so registering here gets -noignore alongside its
	// own flags rather than scoped to one analyzer as -krtequal.noignore would be.
	krtlint.RegisterFlags(flag.CommandLine)
	multichecker.Main(krtlint.Analyzers()...)
}
