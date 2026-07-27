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

package equal

import (
	"sync"

	"pb"

	"istio.io/istio/pkg/kube/krt"
)

type Input struct{ krt.Named }

// Plain contains only fields reflect.DeepEqual handles correctly.
type Plain struct {
	krt.Named
	Ports []int
}

// Compared implements Equals in a form krt can dispatch to.
type Compared struct {
	krt.Named
	Address *pb.Address
}

func (c Compared) Equals(o Compared) bool { return c.Named == o.Named }

// PointerCompared is only ever held by pointer, and declares Equals to match.
type PointerCompared struct {
	krt.Named
	Address *pb.Address
}

func (p *PointerCompared) Equals(o *PointerCompared) bool { return p.Named == o.Named }

// WrongParam declares Equals against an unrelated type, so krt cannot dispatch to it.
type WrongParam struct {
	krt.Named
	Address *pb.Address
}

func (w WrongParam) Equals(o any) bool { return false }

// ValueEquals declares Equals on the value, but the collection holds pointers.
type ValueEquals struct {
	krt.Named
	Address *pb.Address
}

func (v ValueEquals) Equals(o ValueEquals) bool { return v.Named == o.Named }

// WithProto reaches a protobuf message through a nested field.
type WithProto struct {
	krt.Named
	Inner struct{ Address *pb.Address }
}

// WithFunc carries a callback, which reflect.DeepEqual never reports as equal.
type WithFunc struct {
	krt.Named
	OnChange func()
}

// WithMutex carries lock state that reflect.DeepEqual will compare.
type WithMutex struct {
	krt.Named
	mu sync.Mutex
}

// EmbeddedProto promotes ProtoReflect without being a message itself.
type EmbeddedProto struct {
	krt.Named
	*pb.Address
}

func good(c krt.Collection[Input]) {
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *Plain { return nil })
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *Compared { return nil })
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) **PointerCompared { return nil })
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) **pb.Address { return nil })
}

func bad(c krt.Collection[Input]) {
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *WrongParam { return nil })    // want `declares Equals\(o any\) bool, but krt\.Equal cannot dispatch to it`
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) **ValueEquals { return nil })  // want `cannot dispatch to it`
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *WithProto { return nil })     // want `unreliable for protobuf messages \(field Inner\.Address\)`
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *WithFunc { return nil })      // want `unreliable for func values \(field OnChange\)`
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *WithMutex { return nil })     // want `unreliable for synchronization primitives \(field mu\)`
	krt.NewCollection(c, func(ctx krt.HandlerContext, i Input) *EmbeddedProto { return nil }) // want `embeds a protobuf message`
}
