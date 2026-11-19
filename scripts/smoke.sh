#!/usr/bin/env sh
set -eu

BASE_URL="${BASE_URL:-http://127.0.0.1:49983}"
SMOKE_ROOT="${SMOKE_ROOT:-/tmp/envd-smoke-$$}"
FILE_PATH="$SMOKE_ROOT/hello.txt"

curl -fsS "$BASE_URL/health" | grep -q '"status":"ok"'
curl -fsS -X POST \
  -H 'Content-Type: application/json' \
  --data '{}' \
  "$BASE_URL/v1/workspace/restore" >/dev/null
curl -fsS -X POST \
  -H 'Content-Type: application/octet-stream' \
  --data-binary 'envd smoke test' \
  "$BASE_URL/files?path=$FILE_PATH" >/dev/null
[ "$(curl -fsS "$BASE_URL/files?path=$FILE_PATH")" = 'envd smoke test' ]

echo "envd smoke test passed"
