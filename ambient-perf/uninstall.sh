#!/usr/bin/env bash

if ! which helm > /dev/null 2>&1; then
  echo "helm is required"
  exit 1
fi

CHART_NAME="${1}"
if [ -z "${CHART_NAME}" ] || [ ! -f "./tests/${CHART_NAME}/Chart.yaml" ]; then
  echo "Usage: ./uninstall <chart name>"
  echo "Valid Options:"
  find . | grep Chart.yaml | cut -d '/' -f3
  exit 1
fi

RELEASE_NAME="$(helm list -ojson | jq -r ".[] | select(.chart | startswith(\"${CHART_NAME}-\")).name")"
if [ -z "$RELEASE_NAME" ]; then
  echo "No release found for ${CHART_NAME}."
  exit 1
fi

set -x
helm uninstall --wait --cascade foreground "${RELEASE_NAME}"

