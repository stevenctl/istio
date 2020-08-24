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
