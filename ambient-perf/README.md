# perf-tests

Valid tests are `iperf` and `nighthawk`.

## Kind

Charts have some values for setting nodes, so we can make sure capture and
ztunnel are exercised. The defaults correspond to the integration test setup.

- `istio-testing-worker`: clients
- `istio-testing-worker2`: servers
- `istio-testing-control-plane`: waypoints

```bash
# from the istio repo root
export HUB=localhost:5000
export TAG=sandwich-test # if you change it, make sure to update the sandwich chart
./prow/integ-suite-kind.sh --manual --kind-config prow/config/ambient-sc.yaml --skip-cleanup
go run ./istioctl/cmd/istioctl install \
  -f ~/mc-ambient/iop.yaml --set hub=$HUB --set tag=$TAG \
  --set values.global.network=$CLUSTER_NAME --set values.global.imagePullPolicy=Always \
  --manifests manifests/

```

## Tester Setup

```bash
# spin up a client and server
./install.sh <name>
# remove them
./uninstall.sh <name>
```

## Tests

Each test directory contains a `test.sh` 

```bash
./tests/iperf/test.sh
./tests/nighthawk/test.sh
```

```bash
TARGET_HOST="server"
TARGET_PORT="8080"
DURATION=60 # seconds

# nighthawk only
SCHEME="http://"

QPS=100 
CONNECTIONS=100

## NOTE: this multiplies the above
THREADS=1
```

## Sandwich

The "Sandwich" Waypoint is really just a passthrough Envoy (`proxyv2` image, `envoy` executable).
Config could be modified to add some filters we want to test.

Deploy it with `./install sandwich`. It will be put on a node
separate from test clients and servers.

Edit the `values.yaml` to include Pod IPs for the target servers.
By default it will use `LOGICAL_DNS` to headless Service.

To send traffic through it, set `TARGET_HOST`.

```bash
TARGET_HOST=static-waypoint ./tests/iperf/test.sh
TARGET_HOST=static-waypoint ./tests/nighthawk/test.sh
```

