#!/usr/bin/env bash
# Run the Qeet Logs Postman collection with newman.
#
# Usage:
#   ./run.sh                                   # full run against local defaults
#   ./run.sh --api-base http://localhost:8100  # override the query/admin host
#   ./run.sh --ingest-base http://localhost:8101   # override the ingest host
#   ./run.sh --key ql_live_xxx                 # set the API key
#   ./run.sh --folder Query                    # run one folder
#   ./run.sh --folder Query --folder System    # run multiple folders (repeat flag)
#   ./run.sh --bail                            # stop on first failure
#   ./run.sh --ci                              # JUnit + HTML reports under ./reports
#
# Notes:
#   - Requires node >= 18. Newman is fetched on demand via npx.
#   - The "Ingest" folder needs the Rust ingest gateway running on the ingest host;
#     "Prometheus remote_write" and "Live tail (WebSocket)" cannot be driven by newman.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
COLLECTION="$HERE/qeet-logs.postman_collection.json"
ENVIRONMENT="$HERE/qeet-logs.postman_environment.json"

API_BASE=""
INGEST_BASE=""
API_KEY=""
FOLDERS=()
EXTRA=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --api-base)     API_BASE="$2"; shift 2 ;;
    --ingest-base)  INGEST_BASE="$2"; shift 2 ;;
    --key)          API_KEY="$2"; shift 2 ;;
    --folder)       FOLDERS+=("$2"); shift 2 ;;
    --bail)         EXTRA+=("--bail"); shift ;;
    --ci)
      mkdir -p "$HERE/reports"
      EXTRA+=("--reporters" "cli,junit,htmlextra"
              "--reporter-junit-export" "$HERE/reports/qeet-logs.xml"
              "--reporter-htmlextra-export" "$HERE/reports/qeet-logs.html"
              "--reporter-htmlextra-title" "Qeet Logs API")
      shift ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

ARGS=(run "$COLLECTION" -e "$ENVIRONMENT")
[[ -n "$API_BASE" ]]    && ARGS+=(--env-var "apiBaseUrl=$API_BASE")
[[ -n "$INGEST_BASE" ]] && ARGS+=(--env-var "ingestBaseUrl=$INGEST_BASE")
[[ -n "$API_KEY" ]]     && ARGS+=(--env-var "apiKey=$API_KEY")
for f in "${FOLDERS[@]:-}"; do [[ -n "$f" ]] && ARGS+=(--folder "$f"); done
ARGS+=("${EXTRA[@]:-}")

exec npx --yes newman@6 "${ARGS[@]}"
