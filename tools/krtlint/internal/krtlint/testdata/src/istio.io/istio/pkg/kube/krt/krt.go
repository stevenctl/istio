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

// Package krt is a stub of istio.io/istio/pkg/kube/krt for analyzer tests. It mirrors the
// signatures the analyzers key off, without pulling in Kubernetes.
package krt

type Metadata map[string]any

type Collection[T any] interface {
	GetKey(k string) *T
	List() []T
	Register(f func(o Event[T])) HandlerRegistration
	RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration
	Metadata() Metadata
}

type Singleton[T any] interface {
	Get() *T
	Register(f func(o Event[T])) HandlerRegistration
	AsCollection() Collection[T]
	Metadata() Metadata
}

type StaticSingleton[T any] interface {
	Singleton[T]
	Set(*T)
}

type StaticCollection[T any] struct{ vals []T }

func (s StaticCollection[T]) GetKey(k string) *T { return nil }
func (s StaticCollection[T]) List() []T          { return s.vals }
func (s StaticCollection[T]) Register(f func(o Event[T])) HandlerRegistration {
	return nil
}

func (s StaticCollection[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) HandlerRegistration {
	return nil
}
func (s StaticCollection[T]) Metadata() Metadata { return nil }

type Index[K comparable, O any] interface {
	Lookup(k K) []O
	Fetch(ctx HandlerContext, key K, opts ...FetchOption) []O
}

type Event[T any] struct {
	Old, New *T
}

type HandlerRegistration interface{ HasSynced() bool }

type Syncer interface{ HasSynced() bool }

type HandlerContext interface{ DiscardResult() }

type (
	FetchOption      func(*int)
	CollectionOption func(*int)
)

type (
	TransformationEmpty[T any]        func(ctx HandlerContext) *T
	TransformationSingle[I, O any]    func(ctx HandlerContext, i I) *O
	TransformationMulti[I, O any]     func(ctx HandlerContext, i I) []O
	TransformationEmptyToMulti[T any] func(ctx HandlerContext) []T
)

type Equaler[K any] interface{ Equals(k K) bool }

type ResourceNamer interface{ ResourceName() string }

type Named struct{ Name, Namespace string }

func (n Named) ResourceName() string { return n.Namespace + "/" + n.Name }
func (n Named) GetName() string      { return n.Name }
func (n Named) GetNamespace() string { return n.Namespace }

func NewCollection[I, O any](c Collection[I], hf TransformationSingle[I, O], opts ...CollectionOption) Collection[O] {
	return nil
}

func NewManyCollection[I, O any](c Collection[I], hf TransformationMulti[I, O], opts ...CollectionOption) Collection[O] {
	return nil
}

func NewSingleton[O any](hf TransformationEmpty[O], opts ...CollectionOption) Singleton[O] {
	return nil
}

func NewManyFromNothing[O any](hf TransformationEmptyToMulti[O], opts ...CollectionOption) Collection[O] {
	return nil
}

func NewStaticCollection[T any](synced Syncer, vals []T, opts ...CollectionOption) StaticCollection[T] {
	return StaticCollection[T]{}
}

func NewStatic[T any](initial *T, startSynced bool, opts ...CollectionOption) StaticSingleton[T] {
	return nil
}

func MapCollection[T, U any](c Collection[T], fn func(T) U, opts ...CollectionOption) Collection[U] {
	return nil
}

func Fetch[T any](ctx HandlerContext, cc Collection[T], opts ...FetchOption) []T { return nil }

func FetchOne[T any](ctx HandlerContext, c Collection[T], opts ...FetchOption) *T { return nil }

func FetchOrList[T any](ctx HandlerContext, cc Collection[T], opts ...FetchOption) []T { return nil }

func FilterKey(k string) FetchOption { return nil }

func FilterLabel(lbls map[string]string) FetchOption { return nil }

func FilterSelects(lbls map[string]string) FetchOption { return nil }

func FilterSelectsNonEmpty(lbls map[string]string) FetchOption { return nil }
