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
