#!/usr/bin/env bash

TARGET_SCHEME="${TARGET_SCHEME:-http://}"
TARGET_HOST="${TARGET_HOST:-nighthawk-server}"
TARGET_PORT="${TARGET_PORT:-10000}"
DURATION="${DURATION:-60}"
QPS="${QPS:-1000}"
THREADS="${THREADS:-1}"
CONNECTIONS="${CONNECTIONS:-100}"
JITTER="${JITTER:-.00001s}"
OUTPUT_FORMAT="${OUTPUT_FORMAT:-human}"

CLIENT_POD="$(kubectl get po -lapp="nighthawk-client" -ojsonpath='{.items[0].metadata.name}')"
TARGET_URL="${TARGET_SCHEME}${TARGET_HOST}:${TARGET_PORT}"
COMMAND="nighthawk_client ${TARGET_URL} --duration ${DURATION} --rps ${QPS} --concurrency ${THREADS} --connections ${CONNECTIONS} --jitter-uniform ${JITTER} --output-format ${OUTPUT_FORMAT} --request-body-size 500" 

set -x
kubectl exec -it "${CLIENT_POD}" -- $COMMAND
