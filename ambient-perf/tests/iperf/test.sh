#!/usr/bin/env bash

TARGET_HOST="${TARGET_HOST:-iperf-server}"
TARGET_PORT="${TARGET_PORT:-5201}"
DURATION="${DURATION:-60}"
QPS="${QPS}:-1000"

CLIENT_POD="$(kubectl get po -lapp="iperf-client" -ojsonpath='{.items[0].metadata.name}')"
COMMAND="iperf3 -c ${TARGET_HOST} -p ${TARGET_PORT} -t ${DURATION} -O 15 -N"

set -x

kubectl exec -it "${CLIENT_POD}" -- $COMMAND
