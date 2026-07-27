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

// Package pb stands in for generated protobuf code: the analyzers recognize a message by
// its ProtoReflect method rather than by importing the protobuf runtime.
package pb

type Message interface{ isMessage() }

type Address struct {
	Host string

	state int
}

func (a *Address) ProtoReflect() Message { return nil }

func (a *Address) isMessage() {}

// Wrapper stands in for a generated Kubernetes type: not a message itself, but holding one in
// a Spec field, and declared somewhere its consumers cannot add methods to it.
type Wrapper struct {
	Name string
	Spec Address
}
