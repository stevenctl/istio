package connpool

import (
	"fmt"
	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/pilot/common"
	"testing"
	"time"
)

var (
	ist  istio.Instance
	apps common.EchoDeployments
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Setup(istio.Setup(&ist, nil)).
		Setup(func(ctx resource.Context) error {
			return common.SetupApps(ctx, ist, &apps)
		})
}

func TestTraffic(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			for _, c := range apps.PodA {
				dests := apps.PodB
				dest := dests[0]
				tt := common.TrafficTestCase{
					Name:   fmt.Sprintf("%s->%s from %s", c.Config().Service, dest.Config().Service, c.Config().Cluster.Name()),
					Config: "",
					Call: func() (echoclient.ParsedResponses, error) {
						return c.Call(echo.CallOptions{
							Target:   dest,
							PortName: "auto-grpc",
							Scheme:   scheme.GRPC,
							Count:    5 * len(dests),
							Timeout:  time.Second * 5,
						})
					},
					Validator: func(responses echoclient.ParsedResponses) error {
						if err := responses.CheckOK(); err != nil {
							return err
						}
						if err := responses.CheckHost(dest.Config().HostHeader()); err != nil {
							return err
						}
						return nil
					},
					ExpectFailure: false,
					Skip:          false,
				}
				common.ExecuteTrafficTest(ctx, tt, apps.Namespace.Name())
			}
		})
}
