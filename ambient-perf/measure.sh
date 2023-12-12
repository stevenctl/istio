#!/usr/bin/env bash

NAME="${NAME:-testrun}"
mkdir -p results/
rm results/*raw*.json



CASES="${CASES}"
if [ -z "$CASES" ]; then
  CASES="100 500 1000"
fi

for QPS in $CASES; do
  export OUTPUT_FORMAT="json"
  export QPS
  OUTFILE="results/$NAME-raw-$QPS.json"
  ./tests/nighthawk/test.sh | grep -v '^\[' | jq -r "." > $OUTFILE
done

echo "| Scenario | QPS (Client) | QPS (Effective) |     P50 |     P75 |     P90 |     P99 |    P999 |"
echo "|----------|--------------|-----------------|---------|---------|---------|---------|---------|"
for QPS in $CASES; do
  OUTFILE="results/$NAME-raw-$QPS.json"

  RUN_THREADS=$(jq -r .options.concurrency $OUTFILE)
  RUN_QPS=$(jq -r .options.requests_per_second $OUTFILE)
  GLOBAL_QPS=$(($RUN_THREADS * $RUN_QPS))

  JQ_PROG='.results[] | select(.name == "global").statistics[] | select(.id == "benchmark_http_client.latency_2xx")'
  for PERCENTILE in 50 75 90 99 999; do
    JQ_PROG2="[.percentiles[] | select(.percentile >= 0.${PERCENTILE})][0].duration"
    UNROUNDED=$(jq -r "${JQ_PROG}" $OUTFILE | jq -r "${JQ_PROG2}" )
    ROUNDED=$(printf "%.3f" "${UNROUNDED::-1}")
    MILLIS=$(printf "%.0f" $(echo "1000 * $ROUNDED" | bc))
    declare P$PERCENTILE="${MILLIS}ms"
  done

  DURATION="${DURATION:-60}"
  TOTAL_REQ=$(jq -r '.results[] | select(.name == "global").counters[] | select(.name == "upstream_rq_total").value' $OUTFILE)
  TRUE_QPS=$(echo "scale=2; $TOTAL_REQ / $DURATION" | bc)

  printf "| %8s | %12s | %15s | %7s | %7s | %7s | %7s | %7s |\n" $NAME $GLOBAL_QPS $TRUE_QPS "$P50" "$P75" "$P90" "$P99" "$P999"
done

