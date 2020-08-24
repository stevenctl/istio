// +build integ
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

package pilot

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

func TestClusterLocalService(t *testing.T) {
	framework.NewTest(t).
		RequiresMinClusters(2).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("respect-cluster-local-config").Run(func(ctx framework.TestContext) {
				for _, c := range ctx.Clusters() {
					c := c
					ctx.NewSubTest(c.Name()).
						Label(label.Multicluster).
						Run(func(ctx framework.TestContext) {
							local := apps.local.GetOrFail(ctx, echo.InCluster(c))
							if err := local.CallOrFail(ctx, echo.CallOptions{
								Target:   local,
								PortName: "http",
								Count:    callsPerCluster,
							}).CheckReachedClusters(resource.Clusters{c}); err != nil {
								ctx.Fatalf("traffic was not restricted to %s: %v", c.Name(), err)
							}
						})
				}
			})
		})
}
