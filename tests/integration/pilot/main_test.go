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
	"fmt"
	"strconv"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps EchoDeployments

	echoPorts = []echo.Port{
		{Name: "http", Protocol: protocol.HTTP, InstancePort: 18080},
		{Name: "grpc", Protocol: protocol.GRPC, InstancePort: 17070},
		{Name: "tcp", Protocol: protocol.TCP, InstancePort: 19090},
		{Name: "auto-tcp", Protocol: protocol.TCP, InstancePort: 19091},
		{Name: "auto-http", Protocol: protocol.HTTP, InstancePort: 18081},
		{Name: "auto-grpc", Protocol: protocol.GRPC, InstancePort: 17071},
	}
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	namespace namespace.Instance

	// Standard echo app to be used by tests
	podA echo.Instances
	// Standard echo app to be used by tests
	podB echo.Instances
	// Headless echo app to be used by tests
	headless echo.Instances
	// Echo app to be used by tests, with no sidecar injected
	naked echo.Instances
	// A virtual machine echo app
	vmA echo.Instances
}

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		SkipLabel(label.Multicluster).
		Setup(istio.Setup(&i, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  global:
    meshExpansion:
      enabled: true`
		})).
		Setup(func(ctx resource.Context) error {
			var err error
			// TODO: allow using an existing namespace to allow repeated runs with 0 setup
			apps.namespace, err = namespace.New(ctx, namespace.Config{
				Prefix: "echo",
				Inject: true,
			})
			if err != nil {
				return err
			}
			// Headless services don't work with targetPort, set to same port
			headlessPorts := make([]echo.Port, len(echoPorts))
			for i, p := range echoPorts {
				p.ServicePort = p.InstancePort
				headlessPorts[i] = p
			}
			builder := echoboot.NewBuilder(ctx)
			for _, c := range ctx.Clusters() {
				builder.
					With(nil, echo.Config{
						Service:   fmt.Sprintf("a-%d", c.Index()),
						Namespace: apps.namespace,
						Ports:     echoPorts,
						Subsets:   []echo.SubsetConfig{{}},
						Cluster:   c,
					}).
					With(nil, echo.Config{
						Service:   fmt.Sprintf("b-%d", c.Index()),
						Namespace: apps.namespace,
						Ports:     echoPorts,
						Subsets:   []echo.SubsetConfig{{}},
						Cluster:   c,
					}).
					With(nil, echo.Config{
						Service:   fmt.Sprintf("headless-%d", c.Index()),
						Headless:  true,
						Namespace: apps.namespace,
						Ports:     headlessPorts,
						Subsets:   []echo.SubsetConfig{{}},
						Cluster:   c,
					}).
					With(nil, echo.Config{
						Service:    fmt.Sprintf("vm-a-%d", c.Index()),
						Namespace:  apps.namespace,
						Ports:      echoPorts,
						DeployAsVM: true,
						Subsets:    []echo.SubsetConfig{{}},
						Cluster:    c,
					}).
					With(nil, echo.Config{
						Service:   fmt.Sprintf("naked-%d", c.Index()),
						Namespace: apps.namespace,
						Ports:     echoPorts,
						Subsets: []echo.SubsetConfig{
							{
								Annotations: map[echo.Annotation]*echo.AnnotationValue{
									echo.SidecarInject: {
										Value: strconv.FormatBool(false)},
								},
							},
						},
						Cluster: c,
					})
			}
			echos, err := builder.Build()
			if err != nil {
				return err
			}
			apps.podA = echos.Match(echo.ServicePrefix("a-"))
			apps.podB = echos.Match(echo.ServicePrefix("b-"))
			apps.headless = echos.Match(echo.ServicePrefix("headless-"))
			apps.naked = echos.Match(echo.ServicePrefix("naked-"))
			apps.vmA = echos.Match(echo.ServicePrefix("vm-a-"))
			return nil
		}).
		Run()
}

func echoConfig(ns namespace.Instance, name string) echo.Config {
	return echo.Config{
		Service:   name,
		Namespace: ns,
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
		},
		Subsets: []echo.SubsetConfig{{}},
	}
}
