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

package grpcgen

import (
	"net"
	"strconv"
	"strings"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

// Support generation of 'ApiListener' LDS responses, used for native support of gRPC.
// The same response can also be used by other apps using XDS directly.

// GRPC proposal:
// https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md
//
// Note that this implementation is tested against gRPC, but it is generic - any other framework can
// use this XDS mode to get load balancing info from Istio, including MC/VM/etc.

// The corresponding RDS response is also generated - currently gRPC has special differences
// and can't understand normal Istio RDS - in particular expects "" instead of "/" as
// default prefix, and is expects just the route for one host.
// handleAck will detect if the message is an ACK or NACK, and update/log/count
// using the generic structures. "Classical" CDS/LDS/RDS/EDS use separate logic -
// this is used for the API-based LDS and generic messages.

// TransportSocket proto message has a `name` field which is expected to be set
// to this value by the management server.
const transportSocketName = "envoy.transport_sockets.tls"

type GrpcConfigGenerator struct {
}

func clusterKey(hostname string, port int) string {
	return subsetClusterKey("", hostname, port)
}

func subsetClusterKey(subset, hostname string, port int) string {
	return model.BuildSubsetKey(model.TrafficDirectionOutbound, subset, host.Name(hostname), port)
}

func (g *GrpcConfigGenerator) Generate(proxy *model.Proxy, push *model.PushContext,
	w *model.WatchedResource, updates *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	switch w.TypeUrl {
	case v3.ListenerType:
		return g.BuildListeners(proxy, push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	case v3.ClusterType:
		return g.BuildClusters(proxy, push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	case v3.RouteType:
		return g.BuildHTTPRoutes(proxy, push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	}

	return nil, model.DefaultXdsLogDetails, nil
}

// handleLDSApiType handles a LDS request, returning listeners of ApiListener type.
// The request may include a list of resource names, using the full_hostname[:port] format to select only
// specific services.
func (g *GrpcConfigGenerator) BuildListeners(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	resp := model.Resources{}

	filter := map[string]bool{}
	for _, name := range names {
		if strings.Contains(name, ":") {
			n, _, err := net.SplitHostPort(name)
			if err == nil {
				name = n
			}
		}
		filter[name] = true
	}

	for _, el := range node.SidecarScope.EgressListeners {
		for _, sv := range el.Services() {
			shost := string(sv.Hostname)
			if len(filter) > 0 {
				// DiscReq has a filter - only return services that match
				if !filter[shost] {
					continue
				}
			}
			for _, p := range sv.Ports {
				hp := net.JoinHostPort(shost, strconv.Itoa(p.Port))
				ll := &listener.Listener{
					Name: hp,
				}

				ll.Address = &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: sv.Address,
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: uint32(p.Port),
							},
						},
					},
				}
				hcm := &hcm.HttpConnectionManager{
					RouteSpecifier: &hcm.HttpConnectionManager_Rds{
						Rds: &hcm.Rds{
							ConfigSource: &core.ConfigSource{
								ConfigSourceSpecifier: &core.ConfigSource_Ads{
									Ads: &core.AggregatedConfigSource{},
								},
							},
							RouteConfigName: clusterKey(shost, p.Port),
						},
					},
				}
				hcmAny := util.MessageToAny(hcm)
				// TODO: for TCP listeners don't generate RDS, but some indication of cluster name.
				ll.ApiListener = &listener.ApiListener{
					ApiListener: hcmAny,
				}
				resp = append(resp, &discovery.Resource{
					Name:     hp,
					Resource: util.MessageToAny(ll),
				})
			}
		}
	}

	return resp
}

// Handle a gRPC CDS request, used with the 'ApiListener' style of requests.
// The main difference is that the request includes Resources.
func (g *GrpcConfigGenerator) BuildClusters(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	resp := model.Resources{}
	// gRPC doesn't currently support any of the APIs - returning just the expected EDS result.
	// Since the code is relatively strict - we'll add info as needed.
	for _, n := range names {
		hn, portn, err := net.SplitHostPort(n)
		if err != nil {
			log.Warn("Failed to parse ", n, " ", err)
			continue
		}

		porti, err := strconv.Atoi(portn)
		if err != nil {
			log.Warn("Failed to parse ", n, " ", err)
			continue
		}

		// SANS associated with this host name.
		// TODO: apply DestinationRules, etc
		sans := push.ServiceAccounts[host.Name(hn)][porti]

		// Assumes 'default' name, and credentials/tls/certprovider/pemfile

		tlsC := &envoyauth.UpstreamTlsContext{
			CommonTlsContext: &envoyauth.CommonTlsContext{
				TlsCertificateCertificateProviderInstance: &envoyauth.CommonTlsContext_CertificateProviderInstance{
					InstanceName:    "default",
					CertificateName: "default",
				},

				ValidationContextType: &envoyauth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &envoyauth.CommonTlsContext_CombinedCertificateValidationContext{
						ValidationContextCertificateProviderInstance: &envoyauth.CommonTlsContext_CertificateProviderInstance{
							InstanceName:    "default",
							CertificateName: "ROOTCA",
						},
						DefaultValidationContext: &envoyauth.CertificateValidationContext{
							MatchSubjectAltNames: util.StringToExactMatch(sans),
						},
					},
				},
			},
		}

		rc := &cluster.Cluster{
			Name:                 clusterKey(hn, porti),
			ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
			EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
				ServiceName: "outbound|" + portn + "||" + hn,
				EdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
				},
			},
			TransportSocket: &core.TransportSocket{
				Name:       transportSocketName,
				ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tlsC)},
			},
		}
		// see grpc/xds/internal/client/xds.go securityConfigFromCluster
		resp = append(resp, &discovery.Resource{
			Name:     n,
			Resource: util.MessageToAny(rc),
		})
	}
	return resp
}

// handleSplitRDS supports per-VIP routes, as used by GRPC.
// This mode is indicated by using names containing full host:port instead of just port.
// Returns true of the request is of this type.
func (g *GrpcConfigGenerator) BuildHTTPRoutes(node *model.Proxy, push *model.PushContext, routeNames []string) model.Resources {
	resp := model.Resources{}

	// Currently this mode is only used by GRPC, to extract Cluster for the default
	// route.
	for _, n := range routeNames {
		_, _, hostname, port := model.ParseSubsetKey(n)
		if hostname == "" || port == 0 {
			log.Warn("Failed to parse ", n)
			continue
		}
		hn := string(hostname)
		el := node.SidecarScope.GetEgressListenerForRDS(port, "")
		// TODO: use VirtualServices instead !
		// Currently gRPC doesn't support matching the path.
		svc := el.Services()
		for _, s := range svc {
			if s.Hostname.Matches(host.Name(hn)) {
				// Only generate the required route for grpc. Will need to generate more
				// as GRPC adds more features.
				rc := &route.RouteConfiguration{
					Name: n,
					VirtualHosts: []*route.VirtualHost{
						{
							Name:    hn,
							Domains: []string{hn, net.JoinHostPort(hn, strconv.Itoa(port))},

							Routes: []*route.Route{
								{
									Match: &route.RouteMatch{
										PathSpecifier: &route.RouteMatch_Prefix{Prefix: ""},
									},
									Action: &route.Route_Route{
										Route: &route.RouteAction{
											ClusterSpecifier: &route.RouteAction_Cluster{
												Cluster: n,
											},
										},
									},
								},
							},
						},
					},
				}
				resp = append(resp, &discovery.Resource{
					Name:     n,
					Resource: util.MessageToAny(rc),
				})
			}
		}
	}
	return resp
}
