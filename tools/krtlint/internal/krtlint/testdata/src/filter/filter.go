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

package filter

import (
	"istio.io/api/type/v1beta1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/krt"
)

type Bare struct{ krt.Named }

type Labeled struct{ krt.Named }

func (l Labeled) GetLabels() map[string]string { return nil }

type Selecting struct{ krt.Named }

func (s Selecting) GetLabelSelector() map[string]string { return nil }

// SpecSelector relies on krt's reflective Spec.Selector fallback.
type SpecSelector struct {
	krt.Named
	Spec struct{ Selector map[string]string }
}

// WorkloadSelecting uses the other Selector type the fallback accepts.
type WorkloadSelecting struct {
	krt.Named
	Spec struct{ Selector *v1beta1.WorkloadSelector }
}

// WrongSelectorType has a Spec.Selector, but of a type krt's reflection switch rejects.
type WrongSelectorType struct {
	krt.Named
	Spec struct{ Selector *LabelSelector }
}

type LabelSelector struct{ MatchLabels map[string]string }

func supported(ctx krt.HandlerContext, labeled krt.Collection[Labeled], selecting krt.Collection[Selecting], spec krt.Collection[SpecSelector], bare krt.Collection[Bare]) {
	_ = krt.Fetch(ctx, labeled, krt.FilterLabel(nil))
	_ = krt.Fetch(ctx, selecting, krt.FilterSelects(nil))
	_ = krt.Fetch(ctx, spec, krt.FilterSelectsNonEmpty(nil))
	// Key filters have no accessor requirement.
	_ = krt.Fetch(ctx, bare, krt.FilterKey("a/b"))
}

func workloadSelector(ctx krt.HandlerContext, ws krt.Collection[WorkloadSelecting]) {
	_ = krt.Fetch(ctx, ws, krt.FilterSelects(nil))
}

// getLabels asserts the value config.Config only, so a collection of pointers panics.
func configLabels(ctx krt.HandlerContext, byValue krt.Collection[config.Config], byPointer krt.Collection[*config.Config]) {
	_ = krt.Fetch(ctx, byValue, krt.FilterLabel(nil))
	_ = krt.Fetch(ctx, byPointer, krt.FilterLabel(nil)) // want `exposes no labels`
}

func unsupported(ctx krt.HandlerContext, bare krt.Collection[Bare], labeled krt.Collection[Labeled], wrongSel krt.Collection[WrongSelectorType]) {
	_ = krt.Fetch(ctx, bare, krt.FilterLabel(nil))                 // want `krt\.FilterLabel is applied to a collection of filter\.Bare, which exposes no labels`
	_ = krt.FetchOne(ctx, bare, krt.FilterSelects(nil))            // want `krt\.FilterSelects is applied to a collection of filter\.Bare, which exposes no selector`
	_ = krt.FetchOrList(ctx, bare, krt.FilterSelectsNonEmpty(nil)) // want `krt\.FilterSelectsNonEmpty is applied to a collection of filter\.Bare, which exposes no selector`
	// Labels are available here, but a selector is not.
	_ = krt.Fetch(ctx, labeled, krt.FilterSelects(nil)) // want `exposes no selector`
	// A Spec.Selector exists here, but krt's reflection only reads the two types it
	// switches on, and panics on anything else.
	_ = krt.Fetch(ctx, wrongSel, krt.FilterSelects(nil)) // want `exposes no selector`
	// The partial-fetch variants take extra fixed arguments before the options.
	_ = krt.PartialFetch(ctx, bare, func(b Bare) int { return 0 }, func(a, b int) bool { return a == b }, krt.FilterLabel(nil)) // want `exposes no labels`
	_ = krt.PartialFetchComparable(ctx, bare, func(b Bare) int { return 0 }, krt.FilterSelects(nil))                            // want `exposes no selector`
}
