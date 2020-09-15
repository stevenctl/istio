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

package istio

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var (
	mcSamples             = path.Join(env.IstioSrc, "samples", "multicluster")
	exposeIstiodGateway   = path.Join(mcSamples, "expose-istiod.yaml")
	exposeServicesGateway = path.Join(mcSamples, "expose-services.yaml")
	genGatewayScript      = path.Join(mcSamples, "gen-eastwest-gateway.sh")
)

// deployEastWestGateway will create a separate gateway deployment for cross-cluster discovery or cross-network services.
func (i *operatorComponent) deployEastWestGateway(cluster resource.Cluster) error {
	scopes.Framework.Infof("Deploying eastwestgateway in ", cluster.Name())
	// generate k8s resources for the gateway
	cmd := exec.Command(genGatewayScript,
		"--istioNamespace", i.settings.SystemNamespace,
		"--manifests", filepath.Join(env.IstioSrc, "manifests"))
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env,
		"CLUSTER="+cluster.Name(),
		"NETWORK="+cluster.NetworkName())
	if !i.environment.IsMulticluster() {
		cmd.Env = append(cmd.Env, "SINGLE_CLUSTER=1")
	}
	gwYaml, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed generating eastwestgateway manifest for %s: %v", cluster.Name(), err)
	}
	i.saveManifestForCleanup(cluster.Name(), string(gwYaml))
	// push them to the cluster
	if err := i.ctx.Config(cluster).ApplyYAML(i.settings.IngressNamespace, string(gwYaml)); err != nil {
		return fmt.Errorf("failed applying eastwestgateway deployment to %s: %v", cluster.Name(), err)
	}
	return nil
}

func (i *operatorComponent) applyCrossNetworkGateway(cluster resource.Cluster) error {
	scopes.Framework.Infof("Exposing services via eastwestgateway in ", cluster.Name())
	return cluster.ApplyYAMLFiles(i.settings.SystemNamespace, exposeServicesGateway)
}

func (i *operatorComponent) applyIstiodGateway(cluster resource.Cluster) error {
	scopes.Framework.Infof("Exposing istiod via eastwestgateway in ", cluster.Name())
	return cluster.ApplyYAMLFiles(i.settings.SystemNamespace, exposeIstiodGateway)
}
