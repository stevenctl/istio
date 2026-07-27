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

import "istio.io/istio/pkg/kube/krt"

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

func supported(ctx krt.HandlerContext, labeled krt.Collection[Labeled], selecting krt.Collection[Selecting], spec krt.Collection[SpecSelector], bare krt.Collection[Bare]) {
	_ = krt.Fetch(ctx, labeled, krt.FilterLabel(nil))
	_ = krt.Fetch(ctx, selecting, krt.FilterSelects(nil))
	_ = krt.Fetch(ctx, spec, krt.FilterSelectsNonEmpty(nil))
	// Key filters have no accessor requirement.
	_ = krt.Fetch(ctx, bare, krt.FilterKey("a/b"))
}

func unsupported(ctx krt.HandlerContext, bare krt.Collection[Bare], labeled krt.Collection[Labeled]) {
	_ = krt.Fetch(ctx, bare, krt.FilterLabel(nil))                 // want `krt\.FilterLabel is applied to a collection of filter\.Bare, which exposes no labels`
	_ = krt.FetchOne(ctx, bare, krt.FilterSelects(nil))            // want `krt\.FilterSelects is applied to a collection of filter\.Bare, which exposes no selector`
	_ = krt.FetchOrList(ctx, bare, krt.FilterSelectsNonEmpty(nil)) // want `krt\.FilterSelectsNonEmpty is applied to a collection of filter\.Bare, which exposes no selector`
	// Labels are available here, but a selector is not.
	_ = krt.Fetch(ctx, labeled, krt.FilterSelects(nil)) // want `exposes no selector`
}
