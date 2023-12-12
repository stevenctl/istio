#!/usr/bin/env bash

cleanup_dataplane() {
  kubectl label ns default istio-injection-
  kubectl label ns default istio.io/dataplane-mode-
  istioctl x waypoint delete -n default 
  rollout
}

rollout() {
  kubectl rollout restart deploy $(kubectl get deploy --no-headers -o custom-columns=":metadata.name")
  kubectl rollout status deploy
}

scenario_sidecar() {
  kubectl label ns default istio-injection=enabled
  rollout
}

scenario_ztunnel() {
  kubectl label ns default istio.io/dataplane-mode=ambient
}

scenario_waypoint() {
  kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v0.8.1/experimental-install.yaml
  kubectl label ns default istio.io/dataplane-mode=ambient
  istioctl x waypoint apply -n default; sleep 1s
  WAYPOINT_NODE="${WAYPOINT_NODE:-istio-testing-control-plane}"
  PATCH="{\"spec\": {\"template\": {\"spec\": {\"nodeName\": \"$WAYPOINT_NODE\"}}}}"
  kubectl patch deploy namespace-istio-waypoint -p "${PATCH}"
  sleep 1s; kubectl rollout status deploy
}

scenario_sandwich() {
  kubectl label ns default istio.io/dataplane-mode=ambient
  ./install.sh sandwich
  sleep 1s; kubectl rollout status deploy
}

setup_scenario() {
  NAME="${1}"
  case "${NAME}" in
    sidecar) 
      SCENARIO=scenario_sidecar
      ;;
    ztunnel) 
      SCENARIO=scenario_ztunnel
      ;;
    waypoint) 
      SCENARIO=scenario_waypoint
      ;;
    sandwich) 
      SCENARIO=scenario_sandwich
      ;;
    *)
      echo "Invalid option: ${NAME}"
      return 1
      ;;
  esac
  cleanup_dataplane
  $SCENARIO
}

setup_scenario $@
