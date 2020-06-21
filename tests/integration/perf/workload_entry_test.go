package perf

import (
	"fmt"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"testing"
)

var (
	i  istio.Instance
	ns []string
)

const nsCount = 100
var labels = map[string]string{"istio-injection": "enableds"}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		RequireSingleCluster().
		Label(label.Performance).
		Setup(istio.Setup(&i, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  global:
    meshExpansion:
      enabled: true`
		})).
		Setup(func(ctx resource.Context) error {
			for i := 0; i < nsCount; i++ {
				nsName := fmt.Sprintf("test-%d", i)
				ctx.Environment().Clusters()[0].(kube.Cluster).CreateNamespaceWithLabels(nsName, "", labels)
			}
		}).
		Setup(func(ctx resource.Context) error {
			return ctx.ApplyConfig(ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: details-svc
spec:
  hosts:
  - details.bookinfo.com
  location: MESH_INTERNAL
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: details-legacy`)
		}).
		Setup(setupEchos(10)).
		Run()
}

func setupEchos(n int) func(ctx resource.Context) error {
	return func(ctx resource.Context) error {
		builder, err := echoboot.NewBuilder(ctx)
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			var ref echo.Instance
			builder = builder.With(&ref, echo.Config{
				Namespace: ns,
				Service:   fmt.Sprintf("svc-%d", i),
			})
		}
		return builder.Build()
	}
}

func TestWorkloadEntryPerformance(t *testing.T) {
	framework.NewBenchmark(t).
		Repeat(func(ctx framework.BenchmarkContext, i int) error {
			_ = ctx.ApplyConfig(ns.Name(), fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: WorkloadEntry
metadata:
  name: details-svc
spec:
  # use of the service account indicates that the workload has a
  # sidecar proxy bootstrapped with this service account. Pods with
  # sidecars will automatically communicate with the workload using
  # istio mutual TLS.
  serviceAccount: details-legacy
  address: vm%d.vpc01.corp.net
  labels:
    app: details-legacy
    instance-id: vm-%d
`, i, i))
			return nil
		}).
		Run()
}
