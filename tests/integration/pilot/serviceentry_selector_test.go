package pilot

import (
	"fmt"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"testing"
)

const SidecarConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: serviceentry-test
  namespace: %s
spec:
  hosts:
  - serviceentry.test.com
  location: MESH_INTERNAL
  ports:
  - number: 8080
    name: http-8080
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: %s
`

func TestServiceEntryWorkloadSelectors(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "hybrid-vm-pods", Inject: true})

			serviceName := "svc"
			var client, pod, vm echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&client, echo.Config{
					Namespace: ns,
					Service:   "client",
				}).
				With(&pod, echo.Config{
					Namespace: ns,
					Service:   serviceName,
					Version:   "v1",
					Ports: []echo.Port{{
						Name:         "http-8080",
						Protocol:     protocol.HTTP,
						ServicePort:  8080,
						InstancePort: 8080,
					}},
				}).
				With(&vm, echo.Config{
					DeployAsVM: true,
					Namespace:  ns,
					Service:    serviceName,
					Version:    "v2",
					Ports: []echo.Port{{
						Name:         "http-8080",
						Protocol:     protocol.HTTP,
						ServicePort:  8080,
						InstancePort: 8080,
					}},
				}).
				BuildOrFail(ctx)

			ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), fmt.Sprintf(SidecarConfig, ns.Name(), serviceName))

			res := client.CallOrFail(ctx, echo.CallOptions{
				Target:   pod,
				Host:     "serviceentry.test.com",
				Port:     &echo.Port{ServicePort: 8080, InstancePort: 8080, Protocol: protocol.HTTP},
				PortName: "http-8080",
				Scheme:   scheme.HTTP,
				Count:    10,
			})

			c := map[string]int{}
			for _, res := range res {
				c[res.Version]++
			}

			if len(c) != 2 {
				ctx.Fatalf("expected both vm and pod to be reached, versions hit: %v", c)
			}

		})
}
