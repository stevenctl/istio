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

package istio

import (
	"fmt"
	"net"
	"time"

	kubeenv "istio.io/istio/pkg/test/framework/components/environment/kube"

	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	dummyValidationRuleTemplate = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: validation-readiness-dummy-rule
  namespace: %s
spec:
  match: request.headers["foo"] == "bar"
  actions:
  - handler: validation-readiness-dummy
    instances:
    - validation-readiness-dummy
`
)

var (
	ns             = "istio-system"
	igwServiceName = "istio-ingressgateway"
	istiodPort     = 15012
)

func waitForValidationWebhook(accessor *kube.Accessor, cfg Config) error {
	dummyValidationRule := fmt.Sprintf(dummyValidationRuleTemplate, cfg.SystemNamespace)
	defer func() {
		e := accessor.DeleteContents("", dummyValidationRule)
		if e != nil {
			scopes.Framework.Warnf("error deleting dummy rule for waiting the validation webhook: %v", e)
		}
	}()

	scopes.CI.Info("Creating dummy rule to check for validation webhook readiness")
	return retry.UntilSuccess(func() error {
		_, err := accessor.ApplyContents("", dummyValidationRule)
		if err == nil {
			return nil
		}

		return fmt.Errorf("validation webhook not ready yet: %v", err)
	}, retry.Timeout(time.Minute))
}

// TODO(landow) extract this "get nodeport or loadbalancer address" logic
func getIstiodAddress(env *kubeenv.Environment, cluster kubeenv.Cluster) (net.TCPAddr, error) {
	// In KinD, we don't have LoadBalancer support. Instead we do a little bit of trickery to to get the Node
	// port for istiod though the ingressgateway service. The Minikube flag is a misnomer.
	if env.Settings().Minikube {
		pods, err := cluster.GetPods(ns, "istio=ingressgateway")
		if err != nil {
			return net.TCPAddr{}, err
		}

		scopes.Framework.Debugf("Querying ingress, pods:\n%v\n", pods)
		if len(pods) == 0 {
			return net.TCPAddr{}, fmt.Errorf("no ingress pod found")
		}

		scopes.Framework.Debugf("Found pod: \n%v\n", pods[0])
		ip := pods[0].Status.HostIP
		if ip == "" {
			return net.TCPAddr{}, fmt.Errorf("no Host IP available on the ingress node yet")
		}

		svc, err := cluster.GetService(ns, igwServiceName)
		if err != nil {
			return net.TCPAddr{}, err
		}

		scopes.Framework.Debugf("Found service for the gateway:\n%v\n", svc)
		if len(svc.Spec.Ports) == 0 {
			return net.TCPAddr{}, fmt.Errorf("no ports found in service: %s/%s", ns, "istio-ingressgateway")
		}

		var nodePort int32
		for _, svcPort := range svc.Spec.Ports {
			if svcPort.Protocol == "TCP" && svcPort.Port == int32(istiodPort) {
				nodePort = svcPort.NodePort
				break
			}
		}
		if nodePort == 0 {
			return net.TCPAddr{}, fmt.Errorf("no port %d found in service: %s/%s", istiodPort, ns, "istio-ingressgateway")
		}

		return net.TCPAddr{IP: net.ParseIP(ip), Port: int(nodePort)}, nil
	}

	// Otherwise, get the load balancer IP.
	svc, err := cluster.GetService("istio-system", igwServiceName)
	if err != nil {
		return net.TCPAddr{}, err
	}
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return net.TCPAddr{}, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return net.TCPAddr{IP: net.ParseIP(ip), Port: istiodPort}, nil
}
