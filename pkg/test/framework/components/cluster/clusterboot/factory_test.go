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

package clusterboot

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/test/framework/components/cluster"
)

func TestBuild(t *testing.T) {
	tests := []struct {
		config  cluster.Config
		cluster cluster.FakeCluster
	}{
		{
			config: cluster.Config{Kind: cluster.Fake, Name: "auto-fill-primary", Network: "network-0"},
			cluster: cluster.FakeCluster{Topology: cluster.Topology{
				ClusterName: "auto-fill-primary",
				ClusterKind: cluster.Fake,
				// The primary and config clusters should match the cluster name when not specified
				PrimaryClusterName: "auto-fill-primary",
				ConfigClusterName:  "auto-fill-primary",
				Network:            "network-0",
			}},
		},
		{
			config: cluster.Config{Kind: cluster.Fake, Name: "auto-fill-remote", PrimaryClusterName: "auto-fill-primary"},
			cluster: cluster.FakeCluster{Topology: cluster.Topology{
				ClusterName:        "auto-fill-remote",
				ClusterKind:        cluster.Fake,
				PrimaryClusterName: "auto-fill-primary",
				// The config cluster should match the primary cluster when not specified
				ConfigClusterName: "auto-fill-primary",
			}},
		},
		{
			config: cluster.Config{Kind: cluster.Fake, Name: "external-istiod", ConfigClusterName: "remote-config"},
			cluster: cluster.FakeCluster{Topology: cluster.Topology{
				ClusterName:        "external-istiod",
				ClusterKind:        cluster.Fake,
				PrimaryClusterName: "external-istiod",
				ConfigClusterName:  "remote-config",
			}},
		},
		{
			config: cluster.Config{
				Name:               "remote-config",
				Kind:               cluster.Fake,
				PrimaryClusterName: "external-istiod",
				ConfigClusterName:  "remote-config",
			},
			cluster: cluster.FakeCluster{Topology: cluster.Topology{
				ClusterName: "remote-config",
				ClusterKind: cluster.Fake,
				// Explicitly specified in config, should be copied exactly
				PrimaryClusterName: "external-istiod",
				ConfigClusterName:  "remote-config",
			}},
		},
	}
	var clusters cluster.Clusters
	t.Run("build", func(t *testing.T) {
		factory := NewFactory()
		for _, tc := range tests {
			factory = factory.With(tc.config)
		}
		var err error
		clusters, err = factory.Build()
		if err != nil {
			t.Fatal(err)
		}
		if len(clusters) != len(tests) {
			t.Errorf("expcted %d clusters but built %d", len(tests), len(clusters))
		}
	})
	for _, tc := range tests {
		t.Run("built "+tc.config.Name, func(t *testing.T) {
			built := clusters.GetByName(tc.config.Name)
			builtFake := *built.(*cluster.FakeCluster)
			// don't include ref map in comparison
			builtFake.AllClusters = nil
			if diff := cmp.Diff(builtFake, tc.cluster); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestValidation(t *testing.T) {
	tests := map[string][]cluster.Config{
		"empty kind": {
			{Name: "no-kind"},
		},
		"empty name": {
			{Kind: cluster.Fake},
		},
		"duplicate name": {
			{Kind: cluster.Fake, Name: "dupe"},
			{Kind: cluster.Fake, Name: "dupe"},
		},
		"non-existent primary": {
			{Kind: cluster.Fake, Name: "no-primary", PrimaryClusterName: "does-not-exist"},
		},
		"non-existent config": {
			{Kind: cluster.Fake, Name: "no-primary", ConfigClusterName: "does-not-exist"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewFactory().With(tc...).Build()
			if err == nil {
				t.Fatal("expected err but got nil")
			}
		})
	}
}
