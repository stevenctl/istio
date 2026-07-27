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

package fetch

import (
	"time"

	"istio.io/istio/pkg/kube/krt"
)

type Input struct{ krt.Named }

type Output struct{ krt.Named }

func tracked(inputs krt.Collection[Input], others krt.Collection[Output], idx krt.Index[string, Output]) {
	krt.NewCollection(inputs, func(ctx krt.HandlerContext, i Input) *Output {
		_ = krt.Fetch(ctx, others)
		_ = krt.FetchOne(ctx, others, krt.FilterKey(i.Name))
		_ = idx.Fetch(ctx, i.Namespace)
		return nil
	})
}

func untracked(
	inputs krt.Collection[Input],
	others krt.Collection[Output],
	single krt.Singleton[Output],
	idx krt.Index[string, Output],
) {
	krt.NewCollection(inputs, func(ctx krt.HandlerContext, i Input) *Output {
		_ = others.List()                             // want `Collection\.List, which does not register a dependency`
		_ = others.GetKey(i.Name)                     // want `Collection\.GetKey, which does not register a dependency`
		_ = single.Get()                              // want `Singleton\.Get, which does not register a dependency`
		_ = idx.Lookup(i.Namespace)                   // want `Index\.Lookup, which does not register a dependency`
		others.Register(func(o krt.Event[Output]) {}) // want `registers an event handler`
		return nil
	})
}

func nondeterministic(inputs krt.Collection[Input]) {
	krt.NewCollection(inputs, func(ctx krt.HandlerContext, i Input) *Output {
		_ = time.Now() // want `depends on wall-clock time via time\.Now`
		return nil
	})
}

// Helpers that take a HandlerContext are transformations too, wherever they are declared.
func helper(ctx krt.HandlerContext, others krt.Collection[Output]) []Output {
	return others.List() // want `Collection\.List, which does not register a dependency`
}

// Code outside a transformation may read collections directly.
func outside(others krt.Collection[Output]) []Output {
	_ = time.Now()
	return others.List()
}
