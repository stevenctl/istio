// Copyright 2019 Istio Authors
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

package analysis

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

var (
	i istio.Instance
	p pilot.Instance
	g galley.Instance
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite("pilot_analysis_test", m).
		RequireSingleCluster().
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, istio.Setup(&i, func(cfg *istio.Config) {

			cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      PILOT_ENABLE_STATUS: true
  global:
    istiod:
      enabled: true
      enableAnalysis: true
`
		})).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, i, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}
