//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package galley

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

// Instance of Galley
type Instance interface {
	resource.Resource

	// Address of the Galley MCP Server.
	Address() string

	// ApplyConfig applies the given config yaml text via Galley.
	ApplyConfig(ns namespace.Instance, yamlText ...string) error

	// ApplyConfigOrFail applies the given config yaml text via Galley.
	ApplyConfigOrFail(t test.Failer, ns namespace.Instance, yamlText ...string)

	// DeleteConfig deletes the given config yaml text via Galley.
	DeleteConfig(ns namespace.Instance, yamlText ...string) error

	// DeleteConfigOrFail deletes the given config yaml text via Galley.
	DeleteConfigOrFail(t test.Failer, ns namespace.Instance, yamlText ...string)

	// ApplyConfigDir recursively applies all the config files in the specified directory
	ApplyConfigDir(ns namespace.Instance, configDir string) error

	// DeleteConfigDir recursively deletes all the config files in the specified directory
	DeleteConfigDir(ns namespace.Instance, configDir string) error

	// ClearConfig clears all applied config so far.
	ClearConfig() error

	// GetConfigDir returns the current configuration directory.
	GetConfigDir() string

	// SetMeshConfig applies the given mesh config.
	SetMeshConfig(meshCfg string) error

	// SetMeshConfigOrFail calls SetMeshConfig and fails tests if an error is returned.
	SetMeshConfigOrFail(t test.Failer, meshCfg string)

	// WaitForSnapshot waits until the given snapshot is observed for the given type URL.
	WaitForSnapshot(collection string, validator SnapshotValidatorFunc) error

	// WaitForSnapshotOrFail calls WaitForSnapshot and fails the test if it fails.
	WaitForSnapshotOrFail(t test.Failer, collection string, validator SnapshotValidatorFunc)
}

// Config for Galley
type Config struct {

	// SinkAddress to dial-out to, if set.
	SinkAddress string

	// MeshConfig to use for this instance.
	MeshConfig string

	// CreateClient determines if a real connection should be established with Galley. This is a workaround
	// to support Kubernetes environments where Galley is not running.
	// This field is ignored on native
	// TODO(https://github.com/istio/istio/issues/20299) remove this field
	CreateClient bool

	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster
}

// New returns a new instance of echo.
func New(ctx resource.Context, istio istio.Instance, cfg Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Native, func() {
		i, err = newNative(ctx, cfg)
	})
	ctx.Environment().Case(environment.Kube, func() {
		i, err = newKube(ctx, istio, cfg)
	})
	return
}

// NewOrFail returns a new Galley instance, or fails test.
func NewOrFail(t test.Failer, c resource.Context, istio istio.Instance, cfg Config) Instance {
	t.Helper()

	i, err := New(c, istio, cfg)
	if err != nil {
		t.Fatalf("galley.NewOrFail: %v", err)
	}
	return i
}
