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
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/cert/ca"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/yml"
)

type operatorComponent struct {
	id          resource.ID
	settings    Config
	ctx         resource.Context
	environment *kube.Environment
	// installManifest includes the yamls use to install Istio. These can be deleted on cleanup
	// The key is the cluster name
	installManifest map[string]string
}

var _ io.Closer = &operatorComponent{}
var _ Instance = &operatorComponent{}
var _ resource.Dumper = &operatorComponent{}

// ID implements resource.Instance
func (i *operatorComponent) ID() resource.ID {
	return i.id
}

func (i *operatorComponent) Settings() Config {
	return i.settings
}

// When we cleanup, we should not delete CRDs. This will filter out all the crds
func removeCRDs(istioYaml string) string {
	allParts := yml.SplitString(istioYaml)
	nonCrds := make([]string, 0, len(allParts))

	// Make the regular expression multi-line and anchor to the beginning of the line.
	r := regexp.MustCompile(`(?m)^kind: CustomResourceDefinition$`)

	for _, p := range allParts {
		if r.Match([]byte(p)) {
			continue
		}
		nonCrds = append(nonCrds, p)
	}

	return yml.JoinString(nonCrds...)
}

func (i *operatorComponent) Close() (err error) {
	scopes.CI.Infof("=== BEGIN: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)
	defer scopes.CI.Infof("=== DONE: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)
	if i.settings.DeployIstio {
		for _, cluster := range i.environment.KubeClusters {
			if e := cluster.DeleteContents("", removeCRDs(i.installManifest[cluster.Name()])); e != nil {
				err = multierror.Append(err, e)
			}
		}
	}
	return
}

func (i *operatorComponent) Dump() {
	scopes.CI.Errorf("=== Dumping Istio Deployment State...")

	for _, cluster := range i.environment.KubeClusters {
		d, err := i.ctx.CreateTmpDirectory(fmt.Sprintf("istio-state-%s", cluster.Name()))
		if err != nil {
			scopes.CI.Errorf("Unable to create directory for dumping Istio contents: %v", err)
			return
		}

		deployment.DumpPodState(d, i.settings.SystemNamespace, cluster.Accessor)
		deployment.DumpPodEvents(d, i.settings.SystemNamespace, cluster.Accessor)

		pods, err := cluster.GetPods(i.settings.SystemNamespace)
		if err != nil {
			scopes.CI.Errorf("Unable to get pods from the system namespace in cluster %s: %v", cluster.Name(), err)
			return
		}

		for _, pod := range pods {
			for _, container := range pod.Spec.Containers {
				l, err := i.environment.KubeClusters[0].Logs(pod.Namespace, pod.Name, container.Name, false /* previousLog */)
				if err != nil {
					scopes.CI.Errorf("Unable to get logs for pod/container in cluster %s: %s/%s/%s", cluster.Name(),
						pod.Namespace, pod.Name, container.Name)
					continue
				}

				fname := path.Join(d, fmt.Sprintf("%s-%s.log", pod.Name, container.Name))
				if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
					scopes.CI.Errorf("Unable to write logs for pod/container in cluster %s: %s/%s/%s", cluster.Name(),
						pod.Namespace, pod.Name, container.Name)
				}
			}
		}
	}
}

func deploy(ctx resource.Context, env *kube.Environment, cfg Config) (Instance, error) {
	scopes.CI.Infof("=== Istio Component Config ===")
	scopes.CI.Infof("\n%s", cfg.String())
	scopes.CI.Infof("================================")

	i := &operatorComponent{
		environment:     env,
		settings:        cfg,
		ctx:             ctx,
		installManifest: map[string]string{},
	}
	i.id = ctx.TrackResource(i)

	if !cfg.DeployIstio {
		scopes.Framework.Info("skipping deployment as specified in the config")
		return i, nil
	}

	// Top-level work dir for Istio deployment.
	workDir, err := ctx.CreateTmpDirectory("istio-deployment")
	if err != nil {
		return nil, err
	}

	// For multicluster, create and push the CA certs to all clusters to establish a shared root of trust.
	if env.IsMulticluster() {
		if err := deployCACerts(workDir, env, cfg); err != nil {
			return nil, err
		}
	}

	// Generate the istioctl config file
	iopFile := filepath.Join(workDir, "iop.yaml")
	operatorYaml := cfg.IstioOperatorConfigYAML()
	if err := ioutil.WriteFile(iopFile, []byte(operatorYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to write iop: %v", err)
	}

	// Deploy the Istio control plane(s)
	for _, cluster := range env.KubeClusters {
		if env.IsControlPlaneCluster(cluster.Index()) {
			if err := deployControlPlane(i, cfg, cluster, iopFile); err != nil {
				return nil, fmt.Errorf("failed deploying control plane to cluster %d: %v", cluster.Index(), err)
			}
		}
	}
	// Wait for all of the control planes to be started before deploying remote clusters
	for _, cluster := range env.KubeClusters {
		if env.IsControlPlaneCluster(cluster.Index()) {
			if err := waitForControlPlane(i, cluster, cfg); err != nil {
				return nil, err
			}
		}
	}

	// Deploy Istio to remote clusters
	for _, cluster := range env.KubeClusters {
		if !env.IsControlPlaneCluster(cluster.Index()) {
			if err := deployControlPlane(i, cfg, cluster, iopFile); err != nil {
				return nil, fmt.Errorf("failed deploying control plane to cluster %d: %v", cluster.Index(), err)
			}
		}
	}

	if env.IsMulticluster() {
		// For multicluster, configure direct access so each control plane can get endpoints from all
		// API servers.
		if err := configureDirectAPIServerAccess(ctx, env, cfg); err != nil {
			return nil, err
		}
	}

	// Wait for all of the control planes to be started.
	for _, cluster := range env.KubeClusters {
		if err := waitForControlPlane(i, cluster, cfg); err != nil {
			return nil, err
		}
	}

	return i, nil
}

func deployControlPlane(c *operatorComponent, cfg Config, cluster kube.Cluster, iopFile string) (err error) {
	// Create an istioctl to configure this cluster.
	istioCtl, err := istioctl.New(c.ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return err
	}

	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return err
	}
	defaultsIOPFile := cfg.IOPFile
	if !path.IsAbs(defaultsIOPFile) {
		defaultsIOPFile = filepath.Join(env.IstioSrc, defaultsIOPFile)
	}

	installSettings := []string{
		"-f", defaultsIOPFile,
		"-f", iopFile,
		"--set", "values.global.imagePullPolicy=" + s.PullPolicy,
		"--charts", filepath.Join(env.IstioSrc, "manifests"),
	}
	// Include all user-specified values.
	for k, v := range cfg.Values {
		installSettings = append(installSettings, "--set", fmt.Sprintf("values.%s=%s", k, v))
	}
	if c.environment.IsMulticluster() {
		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		installSettings = append(installSettings, "--set", "values.global.multiCluster.clusterName="+cluster.Name())

		if c.environment.IsControlPlaneCluster(cluster.Index()) {
			// Expose Istiod through ingress to allow remote clusters to connect
			installSettings = append(installSettings, "--set", "values.global.meshExpansion.enabled=true")
		} else {
			installSettings = append(installSettings, "--set", "profile=remote")
			controlPlaneCluster := c.environment.GetControlPlaneCluster(cluster).(kube.Cluster)
			istiodAddress, err := getIstiodAddress(c.environment, controlPlaneCluster)
			if err != nil {
				return fmt.Errorf("failed getting the istiod address for cluster %d: %v", controlPlaneCluster.Index(), err)
			}
			installSettings = append(installSettings, "--set", "values.global.remotePilotAddress="+istiodAddress.IP.String())

			// temporarily disable ig so we don't  hang waiting for manifest to apply
			installSettings = append(installSettings, "--set", "components.ingressGateways[0].replicaCount=0")
			defer func() {
				if err != nil {
					return
				}
				if err = replaceIstiodRemoteService(cluster, istiodAddress); err != nil {
					return
				}
				// after istiod-remote is configured we can scale up ingress gateway
				if err = cluster.Accessor.ScaleDeployment(ns, igwServiceName, 1); err != nil {
					return
				}
			}()
		}
	}

	// Save the manifest generate output so we can later cleanup
	genCmd := []string{"manifest", "generate"}
	genCmd = append(genCmd, installSettings...)
	out, _, err := istioCtl.Invoke(genCmd)
	if err != nil {
		return err
	}
	c.installManifest[cluster.Name()] = out

	// Actually run the manifest apply command
	cmd := []string{
		"manifest", "apply",
		"--skip-confirmation",
		"--wait",
	}
	cmd = append(cmd, installSettings...)
	scopes.CI.Infof("Running istio control plane on cluster %s %v", cluster.Name(), cmd)
	if _, _, err := istioCtl.Invoke(cmd); err != nil {
		return fmt.Errorf("manifest apply failed: %v", err)
	}

	return nil
}

// replaceIstiodRemoteService deletes and recreates the istiod-remote Service and Endpoint with two key differences:
// - The Service does not user `ClusterIP: None` so we can map 15012 to the configured port as `targetPort`
// - The Endpoint uses the given port rather than the default 15012
func replaceIstiodRemoteService(cluster kube.Cluster, address net.TCPAddr) error {
	if err := cluster.Accessor.DeleteService("istiod-remote", "istio-system"); err != nil {
		return fmt.Errorf("failed deleting istiod-remote in cluster %d: %v", cluster.Index(), err)
	}

	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: istiod-remote
  namespace: istio-system
spec:
  ports:
  - name: tcp-istiod
    port: 15012
    protocol: TCP
    targetPort: %d
  sessionAffinity: None
  type: ClusterIP
---
apiVersion: v1
kind: Endpoints
metadata:
  name: istiod-remote
  namespace: istio-system
subsets:
- addresses:
  - ip: %s
  ports:
  - name: tcp-istiod
    port: %d
    protocol: TCP
`, address.Port, address.IP, address.Port)
	if _, err := cluster.Accessor.ApplyContents("istio-system", manifest); err != nil {
		return fmt.Errorf("failed creating istiod-remote ClusterIP service in cluster %d: %v", cluster.Index(), err)
	}
	return nil
}

func waitForControlPlane(dumper resource.Dumper, cluster kube.Cluster, cfg Config) error {
	if !cfg.SkipWaitForValidationWebhook {
		// Wait for webhook to come online. The only reliable way to do that is to see if we can submit invalid config.
		if err := waitForValidationWebhook(cluster.Accessor, cfg); err != nil {
			dumper.Dump()
			return err
		}
	}
	return nil
}

func configureDirectAPIServerAccess(ctx resource.Context, env *kube.Environment, cfg Config) error {
	// Configure direct access for each control plane to each APIServer. This allows each control plane to
	// automatically discover endpoints in remote clusters.
	for _, cluster := range env.KubeClusters {
		// Create a secret.
		secret, err := createRemoteSecret(ctx, cluster)
		if err != nil {
			return fmt.Errorf("failed creating remote secret for cluster %s: %v", cluster.Name(), err)
		}

		// Copy this secret to all control plane clusters.
		for _, remote := range env.ControlPlaneClusters() {
			if cluster.Index() != remote.Index() {
				if _, err := remote.ApplyContents(cfg.SystemNamespace, secret); err != nil {
					return fmt.Errorf("failed applying remote secret to cluster %s: %v", remote.Name(), err)
				}
			}
		}
	}
	return nil
}

func createRemoteSecret(ctx resource.Context, cluster kube.Cluster) (string, error) {
	istioCtl, err := istioctl.New(ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return "", err
	}
	cmd := []string{
		"x", "create-remote-secret",
		"--name", cluster.Name(),
	}

	scopes.CI.Infof("Creating remote secret for cluster cluster %d %v", cluster.Index(), cmd)
	out, _, err := istioCtl.Invoke(cmd)
	if err != nil {
		return "", fmt.Errorf("create remote secret failed for cluster %d: %v", cluster.Index(), err)
	}
	return out, nil
}

func deployCACerts(workDir string, env *kube.Environment, cfg Config) error {
	certsDir := filepath.Join(workDir, "cacerts")
	if err := os.Mkdir(certsDir, 0700); err != nil {
		return err
	}

	root, err := ca.NewRoot(certsDir)
	if err != nil {
		return fmt.Errorf("failed creating the root CA: %v", err)
	}

	for _, cluster := range env.KubeClusters {
		// Create a subdir for the cluster certs.
		clusterDir := filepath.Join(certsDir, cluster.Name())
		if err := os.Mkdir(clusterDir, 0700); err != nil {
			return err
		}

		// Create the new extensions config for the CA
		caConfig, err := ca.NewIstioConfig(cfg.SystemNamespace)
		if err != nil {
			return err
		}

		// Create the certs for the cluster.
		clusterCA, err := ca.NewIntermediate(clusterDir, caConfig, root)
		if err != nil {
			return fmt.Errorf("failed creating intermediate CA for cluster %s: %v", cluster.Name(), err)
		}

		// Create the CA secret for this cluster. Istio will use these certs for its CA rather
		// than its autogenerated self-signed root.
		secret, err := clusterCA.NewIstioCASecret()
		if err != nil {
			return fmt.Errorf("failed creating intermediate CA secret for cluster %s: %v", cluster.Name(), err)
		}

		// Create the system namespace.
		if err := cluster.CreateNamespace(cfg.SystemNamespace, ""); err != nil {
			scopes.CI.Infof("failed creating namespace %s on cluster %s. This can happen when deploying "+
				"multiple control planes. Error: %v", cfg.SystemNamespace, cluster.Name(), err)
		}

		// Create the secret for the cacerts.
		if err := cluster.CreateSecret(cfg.SystemNamespace, secret); err != nil {
			scopes.CI.Infof("failed to create CA secrets on cluster %s. This can happen when deploying "+
				"multiple control planes. Error: %v", cluster.Name(), err)
		}
	}
	return nil
}
