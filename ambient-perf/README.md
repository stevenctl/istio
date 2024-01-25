# perf-tests

Valid tests are `iperf` and `nighthawk`.

## Kind

Charts have some values for setting nodes, so we can make sure capture and
ztunnel are exercised. The defaults correspond to the integration test setup.

- `istio-testing-worker`: clients
- `istio-testing-worker2`: servers
- `istio-testing-control-plane`: waypoints

If you need to rebuild images:

```bash
export HUB=localhost:5000; export TAG=sandwich-test

cd $ZTUNNEL
git fetch; git checkout stevenctl/sandwich-bench
make build

cd $ISTIO
git fetch; git checkout stevenctl/sandwich-bench
cp $ZTUNNEL/out/rust/debug/ztunnel $ISTIO/out/linux_amd64/
cp $ZTUNNEL/out/rust/debug/ztunnel $ISTIO/out/linux_amd64/release

./tools/docker --builder crane --push --targets pilot,proxyv2,install-cni,solo-install-cni,ztunnel
```

Then setup a cluster and install istio

```bash
# from the istio repo root
export HUB=localhost:5000
export TAG=sandwich-test # if you change it, make sure to update the sandwich chart

kind create cluster --config=- <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ambient
nodes:
- role: control-plane
  extraMounts:
  - hostPath: /tmp/control-plane-ztunnel/
    containerPath: /var/run/ztunnel/
- role: worker
  extraMounts:
  - hostPath: /tmp/worker1-ztunnel/
    containerPath: /var/run/ztunnel/
- role: worker
  extraMounts:
  - hostPath: /tmp/worker2-ztunnel/
    containerPath: /var/run/ztunnel/
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:5000"]
    endpoint = ["http://kind-registry:5000"]
EOF

go run ./istioctl/cmd/istioctl install \
  -f ambient-perf/iop.yaml --set hub=$HUB --set tag=$TAG \
  --set values.global.imagePullPolicy=Always \
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
