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

package equalsfields

import (
	"reflect"

	"istio.io/istio/pkg/kube/krt"
)

type Complete struct {
	krt.Named
	Port int
}

func (c Complete) Equals(o Complete) bool {
	return c.Named == o.Named && c.Port == o.Port
}

type Missing struct {
	krt.Named
	Port    int
	Enabled bool
}

func (m Missing) Equals(o Missing) bool { // want `equalsfields\.Missing\.Equals does not compare Port, Enabled`
	return m.Named == o.Named
}

// Selector is an embedded struct. Comparing the promoted Labels field does compare the
// embedded field, so nothing is missing here.
type Selector struct{ Labels map[string]string }

type Promoted struct {
	krt.Named
	Selector
}

func (p Promoted) Equals(o Promoted) bool {
	return p.Named == o.Named && len(p.Labels) == len(o.Labels)
}

// Lopsided reads Port from the receiver only, so the argument's Port is never consulted.
type Lopsided struct {
	krt.Named
	Port int
}

func (l Lopsided) Equals(o Lopsided) bool { // want `equalsfields\.Lopsided\.Equals reads Port from only one operand`
	if l.Port < 0 {
		return false
	}
	return l.Named == o.Named
}

// SelfLookup ranges over the receiver's map and then indexes the same map instead of the
// argument's, so the comparison can never fail.
type SelfLookup struct {
	krt.Named
	Ports map[string]int
}

func (s SelfLookup) Equals(o SelfLookup) bool {
	if len(s.Ports) != len(o.Ports) {
		return false
	}
	for k, v1 := range s.Ports {
		if v2, ok := s.Ports[k]; !ok || v1 != v2 { // want `indexes s\.Ports inside a loop over that same field`
			return false
		}
	}
	return s.Named == o.Named
}

// PairedLookup indexes both operands inside the loop, which is the correct form.
type PairedLookup struct {
	krt.Named
	Ports []int
}

func (p PairedLookup) Equals(o PairedLookup) bool {
	if len(p.Ports) != len(o.Ports) {
		return false
	}
	for i := range p.Ports {
		if p.Ports[i] != o.Ports[i] {
			return false
		}
	}
	return p.Named == o.Named
}

// Todo carries a known-gap marker, which is suppressed unless -todos is set.
type Todo struct {
	krt.Named
	// +krtEqualsTodo compare the class name once churn is understood
	Class string
}

func (t Todo) Equals(o Todo) bool {
	return t.Named == o.Named
}

type Ignored struct {
	krt.Named
	Port int
	// Marshaled is a cache of the fields above.
	// +noKrtEquals
	Marshaled []byte
}

func (i Ignored) Equals(o Ignored) bool {
	return i.Named == o.Named && i.Port == o.Port
}

// Delegating passes whole values to a helper, so per-field attribution is impossible and
// the check backs off rather than guess.
type Delegating struct {
	krt.Named
	Port int
}

func (d Delegating) Equals(o Delegating) bool {
	return reflect.DeepEqual(d, o)
}

// Undispatchable is reported by the krtequal analyzer instead, not here.
type Undispatchable struct {
	krt.Named
	Port int
}

func (u Undispatchable) Equals(o any) bool { return false }
