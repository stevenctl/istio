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

// Scoped names this analyzer specifically, so the field is exempt here.
type Scoped struct {
	krt.Named
	// Class is not worth comparing yet.
	//nokrtlint:krtequalsfields -- churn is not understood
	Class string
}

func (t Scoped) Equals(o Scoped) bool {
	return t.Named == o.Named
}

// Elsewhere names a different analyzer, so this field is still reported.
type Elsewhere struct {
	krt.Named
	//nokrtlint:krtfetch -- unrelated to this check
	Class string
}

func (e Elsewhere) Equals(o Elsewhere) bool { // want `equalsfields\.Elsewhere\.Equals does not compare Class`
	return e.Named == o.Named
}

type Ignored struct {
	krt.Named
	Port int
	// Marshaled is a cache of the fields above.
	//nokrtlint
	Marshaled []byte
}

func (i Ignored) Equals(o Ignored) bool {
	return i.Named == o.Named && i.Port == o.Port
}

// Keyed builds its key from Source and Index, so neither can differ between two objects
// Equals is asked about: a change to either produces a different key, and krt delivers that
// as a delete and an add. Only Extra is genuinely uncompared.
type Keyed struct {
	Source string
	Index  int
	Extra  string
	Value  int
}

func (k Keyed) ResourceName() string { return k.Source + "/" + string(rune(k.Index)) }

func (k Keyed) Equals(o Keyed) bool { // want `equalsfields\.Keyed\.Equals does not compare Extra`
	return k.Value == o.Value
}

// KeyedIndirect reaches its key fields through a helper. The field selection still happens in
// ResourceName, so the exemption holds.
type KeyedIndirect struct {
	Source string
	Value  int
}

func keyOf(s string) string { return "prefix/" + s }

func (k KeyedIndirect) ResourceName() string { return keyOf(k.Source) }

func (k KeyedIndirect) Equals(o KeyedIndirect) bool {
	return k.Value == o.Value
}

// KeyedWhole hands its whole receiver to a helper, so which fields reach the key cannot be
// told and nothing is exempted.
type KeyedWhole struct {
	krt.Named
	Source string
	Value  int
}

func nameOf(k KeyedWhole) string { return k.Source }

func (k KeyedWhole) ResourceName() string { return nameOf(k) }

func (k KeyedWhole) Equals(o KeyedWhole) bool { // want `equalsfields\.KeyedWhole\.Equals does not compare Source`
	return k.Named == o.Named && k.Value == o.Value
}

// Identity compares by identity on purpose; the directive excuses the whole method. The
// directive sits inside a doc comment whose reason runs on past it, which still reaches the
// declaration because scoping is by comment group.
type Identity struct {
	krt.Named
	Client  *int
	Handler func()
}

// Equals compares two Identity values. The rest of the struct is runtime plumbing hung off
// the thing Named identifies, not state a comparison could read.
//
//nokrtlint:krtequalsfields -- identity equality on purpose, see above
func (i Identity) Equals(o Identity) bool {
	return i.Named == o.Named
}

// Trailing carries the directive on the reported line rather than above it.
type Trailing struct {
	krt.Named
	Port int
}

func (t Trailing) Equals(o Trailing) bool { //nokrtlint -- deliberately partial
	return t.Named == o.Named
}

// Unrelated names a different analyzer, so this diagnostic still fires.
type Unrelated struct {
	krt.Named
	Port int
}

//nokrtlint:krtfetch -- unrelated to this check
func (u Unrelated) Equals(o Unrelated) bool { // want `equalsfields\.Unrelated\.Equals does not compare Port`
	return u.Named == o.Named
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
