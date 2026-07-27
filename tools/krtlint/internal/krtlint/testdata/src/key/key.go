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

package key

import "istio.io/istio/pkg/kube/krt"

type Input struct{ krt.Named }

// Keyed is keyed by a value-receiver ResourceName, which is what krt requires.
type Keyed struct{ Name string }

func (k Keyed) ResourceName() string { return k.Name }

// Unkeyed provides no way to derive a key.
type Unkeyed struct{ Name string }

// PointerKeyed declares ResourceName on the pointer receiver only.
type PointerKeyed struct{ Name string }

func (p *PointerKeyed) ResourceName() string { return p.Name }

// Host is a defined string type. krt asserts `any(a).(string)`, which a defined type
// does not satisfy.
type Host string

func good(c krt.Collection[Input]) {
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *Keyed { return nil })
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *string { return nil })
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) **PointerKeyed { return nil })
	krt.NewSingleton(func(ctx krt.HandlerContext) *Keyed { return nil })
	// Statics hold a single value directly and never derive a key.
	krt.NewStatic(&Unkeyed{}, true)
}

func bad(c krt.Collection[Input]) {
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *Unkeyed { return nil }) // want `element type key\.Unkeyed has no key`
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *Host { return nil })    // want `element type key\.Host has no key`
	krt.NewManyCollection(c, func(ctx krt.HandlerContext, i Input) []Unkeyed {          // want `element type key\.Unkeyed has no key`
		return nil
	})
	// ResourceName exists, but only on *PointerKeyed, so the value never satisfies it.
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *PointerKeyed { return nil }) // want `ResourceName is declared on \*key\.PointerKeyed`
}

// generic helpers are the caller's responsibility and are not checked.
func generic[T any](c krt.Collection[Input], fn func(ctx krt.HandlerContext, i Input) *T) krt.Collection[T] {
	return krt.NewCollection(c, fn)
}
