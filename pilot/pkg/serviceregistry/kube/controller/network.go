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

package controller

import (
	"net"
	"sync"

	"github.com/yl2chen/cidranger"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/network"
)

type networkManager struct {
	sync.RWMutex
	// CIDR ranger based on path-compressed prefix trie
	ranger              cidranger.Ranger
	clusterID           cluster.ID
	meshNetworksWatcher mesh.NetworksWatcher

	// Network name for to be used when the meshNetworks fromRegistry nor network label on pod is specified
	// This is defined by a topology.istio.io/network label on the system namespace.
	network network.ID
	// Network name for the registry as specified by the MeshNetworks configmap
	networkFromMeshConfig network.ID
	// map of svc fqdn to partially built network gateways; the actual gateways will be built from these into networkGatewaysBySvc
	// this map just enumerates which networks/ports each Service is a gateway for
	registryServiceNameGateways map[host.Name][]model.NetworkGateway
	// gateways for each service
	networkGatewaysBySvc map[host.Name]model.NetworkGatewaySet
	// implements NetworkGatewaysWatcher; we need to call c.NotifyGatewayHandlers when our gateways change
	model.NetworkGatewaysHandler
}

func initNetworkManager(options Options) networkManager {
	return networkManager{
		clusterID:           options.ClusterID,
		meshNetworksWatcher: options.MeshNetworksWatcher,
		// zero values are a workaround structcheck issue: https://github.com/golangci/golangci-lint/issues/826
		ranger:                      nil,
		network:                     "",
		networkFromMeshConfig:       "",
		registryServiceNameGateways: make(map[host.Name][]model.NetworkGateway),
		networkGatewaysBySvc:        make(map[host.Name]model.NetworkGatewaySet),
	}
}

// setNetworkFromNamespace sets network got from system namespace, returns whether it has changed
func (n *networkManager) setNetworkFromNamespace(ns *v1.Namespace) bool {
	nw := ns.Labels[label.TopologyNetwork.Name]
	n.Lock()
	defer n.Unlock()
	oldDefaultNetwork := n.network
	n.network = network.ID(nw)
	return oldDefaultNetwork != n.network
}

func (n *networkManager) networkFromSystemNamespace() network.ID {
	n.RLock()
	defer n.RUnlock()
	return n.network
}

func (n *networkManager) networkFromMeshNetworks(endpointIP string) network.ID {
	n.RLock()
	defer n.RUnlock()
	if n.networkFromMeshConfig != "" {
		return n.networkFromMeshConfig
	}

	if n.ranger != nil {
		ip := net.ParseIP(endpointIP)
		if ip == nil {
			return ""
		}
		entries, err := n.ranger.ContainingNetworks(ip)
		if err != nil {
			log.Errorf("error getting cidr ranger entry from endpoint ip %s", endpointIP)
			return ""
		}
		if len(entries) > 1 {
			log.Warnf("Found multiple networks CIDRs matching the endpoint IP: %s. Using the first match.", endpointIP)
		}
		if len(entries) > 0 {
			return (entries[0].(namedRangerEntry)).name
		}
	}
	return ""
}

// namedRangerEntry for holding network's CIDR and name
type namedRangerEntry struct {
	name    network.ID
	network net.IPNet
}

// Network returns the IPNet for the network
func (n namedRangerEntry) Network() net.IPNet {
	return n.network
}

// onNetworkChange is fired if the default network is changed either via the namespace label or mesh-networks
func (c *Controller) onNetworkChange() {
	// the network for endpoints are computed when we process the events; this will fix the cache
	// NOTE: this must run before the other network watcher handler that creates a force push
	if err := c.syncPods(); err != nil {
		log.Errorf("one or more errors force-syncing pods: %v", err)
	}
	if err := c.endpoints.sync("", metav1.NamespaceAll, model.EventAdd, true); err != nil {
		log.Errorf("one or more errors force-syncing endpoints: %v", err)
	}
	c.reloadNetworkGateways()
}

// reloadMeshNetworks will read the mesh networks configuration to setup
// fromRegistry and cidr based network lookups for this registry
func (n *networkManager) reloadMeshNetworks() {
	n.Lock()
	defer n.Unlock()
	n.networkFromMeshConfig = ""
	ranger := cidranger.NewPCTrieRanger()

	n.networkFromMeshConfig = ""
	n.registryServiceNameGateways = make(map[host.Name][]model.NetworkGateway)

	meshNetworks := n.meshNetworksWatcher.Networks()
	if meshNetworks == nil || len(meshNetworks.Networks) == 0 {
		return
	}
	for id, v := range meshNetworks.Networks {
		// track endpoints items from this registry are a part of this network
		fromRegistry := false
		for _, ep := range v.Endpoints {
			if ep.GetFromCidr() != "" {
				_, nw, err := net.ParseCIDR(ep.GetFromCidr())
				if err != nil {
					log.Warnf("unable to parse CIDR %q for network %s", ep.GetFromCidr(), id)
					continue
				}
				rangerEntry := namedRangerEntry{
					name:    network.ID(id),
					network: *nw,
				}
				_ = ranger.Insert(rangerEntry)
			}
			if ep.GetFromRegistry() != "" && cluster.ID(ep.GetFromRegistry()) == n.clusterID {
				fromRegistry = true
			}
		}

		// fromRegistry field specified this cluster
		if fromRegistry {
			// treat endpoints in this cluster as part of this network
			if n.networkFromMeshConfig != "" {
				log.Warnf("multiple networks specify %s in fromRegistry; endpoints from %s will continue to be treated as part of %s",
					n.clusterID, n.clusterID, n.networkFromMeshConfig)
			} else {
				n.networkFromMeshConfig = network.ID(id)
			}

			// services in this registry matching the registryServiceName and port are part of this network
			for _, gw := range v.Gateways {
				if gwSvcName := gw.GetRegistryServiceName(); gwSvcName != "" {
					svc := host.Name(gwSvcName)
					n.registryServiceNameGateways[svc] = append(n.registryServiceNameGateways[svc], model.NetworkGateway{
						Network: network.ID(id),
						Cluster: n.clusterID,
						Port:    gw.GetPort(),
					})
				}
			}
		}

	}
	n.ranger = ranger
}

func (c *Controller) NetworkGateways() []model.NetworkGateway {
	c.networkManager.RLock()
	defer c.networkManager.RUnlock()

	if len(c.networkGatewaysBySvc) == 0 {
		return nil
	}

	// Merge all the gateways into a single set to eliminate duplicates.
	out := make(model.NetworkGatewaySet)
	for _, gateways := range c.networkGatewaysBySvc {
		out.AddAll(gateways)
	}

	return out.ToArray()
}

// extractGatewaysFromService checks if the service is a cross-network gateway
// and if it is, updates the controller's gateways.
func (c *Controller) extractGatewaysFromService(svc *model.Service) bool {
	changed := c.extractGatewaysInner(svc)
	if changed {
		c.NotifyGatewayHandlers()
	}
	return changed
}

// reloadNetworkGateways performs extractGatewaysFromService for all services registered with the controller.
// It is called only by `onNetworkChange`.
// It iterates over all services, because mesh networks can be set with a service name.
func (c *Controller) reloadNetworkGateways() {
	c.Lock()
	gwsChanged := false
	for _, svc := range c.servicesMap {
		if c.extractGatewaysInner(svc) {
			gwsChanged = true
			break
		}
	}
	c.Unlock()
	if gwsChanged {
		c.NotifyGatewayHandlers()
		// TODO ConfigUpdate via gateway handler
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.NetworksTrigger}})
	}
}

// extractGatewaysInner performs the logic for extractGatewaysFromService without locking the controller.
// Returns true if any gateways changed.
func (n *networkManager) extractGatewaysInner(svc *model.Service) bool {
	n.Lock()
	defer n.Unlock()
	previousGateways := n.networkGatewaysBySvc[svc.Hostname]

	// label based gateways
	newGateways := svc.NetworkGateways(n.clusterID)
	// meshNetworks registryServiceName+fromRegistry (this loop should rarely hit)
	for _, meshNetworksGateway := range n.registryServiceNameGateways[svc.Hostname] {
		newGateways.AddAll(svc.NetworkGatewaysWithAddresses(meshNetworksGateway))
	}

	gatewaysChanged := !newGateways.Equals(previousGateways)
	if len(newGateways) > 0 {
		n.networkGatewaysBySvc[svc.Hostname] = newGateways
	} else {
		delete(n.networkGatewaysBySvc, svc.Hostname)
	}

	return gatewaysChanged
}

// updateServiceNodePortAddresses updates ClusterExternalAddresses for Services of nodePort type
func (c *Controller) updateServiceNodePortAddresses(svcs ...*model.Service) bool {
	// node event, update all nodePort gateway services
	if len(svcs) == 0 {
		svcs = c.getNodePortGatewayServices()
	}
	// no nodePort gateway service found, no update
	if len(svcs) == 0 {
		return false
	}
	for _, svc := range svcs {
		c.RLock()
		nodeSelector := c.nodeSelectorsForServices[svc.Hostname]
		c.RUnlock()
		// update external address
		var nodeAddresses []string
		for _, n := range c.nodeInfoMap {
			if nodeSelector.SubsetOf(n.labels) {
				nodeAddresses = append(nodeAddresses, n.address)
			}
		}
		if svc.Attributes.ClusterExternalAddresses == nil {
			svc.Attributes.ClusterExternalAddresses = &model.AddressMap{}
		}
		svc.Attributes.ClusterExternalAddresses.SetAddressesFor(c.Cluster(), nodeAddresses)
		// update gateways that use the service
		c.extractGatewaysFromService(svc)
	}
	return true
}

// getNodePortServices returns nodePort type gateway service
func (c *Controller) getNodePortGatewayServices() []*model.Service {
	c.RLock()
	defer c.RUnlock()
	out := make([]*model.Service, 0, len(c.nodeSelectorsForServices))
	for hostname := range c.nodeSelectorsForServices {
		svc := c.servicesMap[hostname]
		if svc != nil {
			out = append(out, svc)
		}
	}

	return out
}
