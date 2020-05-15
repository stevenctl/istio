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

package multicluster

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

// ClusterLocalTest tests that traffic works within a local cluster while in a multicluster configuration
// clusterLocalNS have been configured in meshConfig.serviceSettings to be clusterLocal.
func ClusterLocalTest(t *testing.T, clusterLocalNS namespace.Instance, pilots []pilot.Instance) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("respect-cluster-local-config").Run(func(ctx framework.TestContext) {
				clusters := ctx.Environment().Clusters()
				for i := range clusters {
					ctx.NewSubTest(fmt.Sprintf("cluster-%d cluster local", i)).
						RunParallel(func(ctx framework.TestContext) {
							local := clusters[i]
							remotes := getRemoteClusters(clusters, i)

							// Deploy a only in local, but b in all clusters.
							var a1, b1 echo.Instance
							builder := echoboot.NewBuilderOrFail(ctx, ctx).
								With(&a1, newEchoConfig("a", clusterLocalNS, local, pilots)).
								With(&b1, newEchoConfig("b", clusterLocalNS, local, pilots))
							for _, remoteCluster := range remotes {
								var ref echo.Instance
								builder = builder.With(&ref, newEchoConfig("b", clusterLocalNS, remoteCluster, pilots))
							}
							builder.BuildOrFail(ctx)

							results := callOrFail(ctx, a1, b1)

							// Ensure that all requests went to the local cluster.
							results.CheckClusterOrFail(ctx, fmt.Sprintf("%d", local.Index()))
						})
				}
			})
		})
}

func getRemoteClusters(clusters []resource.Cluster, local int) []resource.Cluster {
	i := 0
	remotes := make([]resource.Cluster, len(clusters)-1)
	for j := range clusters {
		if j == local {
			continue
		}
		remotes[i] = clusters[j]
		i++
	}
	return remotes
}
