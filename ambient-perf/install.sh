#!/usr/bin/env bash

if ! which helm > /dev/null 2>&1; then
  echo "helm is required"
  exit 1
fi

CHART_NAME="${1}"
if [ -z "${CHART_NAME}" ] || [ ! -f "./tests/${CHART_NAME}/Chart.yaml" ]; then
  echo "Usage: ./install <chart name>"
  echo "Valid Options:"
  find . | grep Chart.yaml | cut -d '/' -f3
  exit 1
fi

RELEASE_NAME="$(helm list -ojson | jq -r ".[] | select(.chart | startswith(\"${CHART_NAME}-\")).name")"
echo "${CHART_NAME} -> ${RELEASE_NAME}"

[ -n "$RELEASE_NAME" ] && CMD="upgrade ${RELEASE_NAME}" || CMD="install --generate-name"
[ -n "$VALUES" ] && CMD="$CMD -f ${VALUES}"
set -x
helm $CMD ./tests/$1

