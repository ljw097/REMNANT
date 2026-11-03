#!/usr/bin/env bash
# Run REMNANT on your own machine with one command.
#
#   scripts/local.sh
#
# It builds the web UI and remnantd, then runs `remnantd local`, which fills
# local defaults (a per-user data directory, a master key file, loopback
# only, updates off), brings up a PostgreSQL in Docker when
# REMNANT_DATABASE_URL is not set, applies migrations, serves, and opens the
# first-run setup page in your browser. Stop it with Ctrl-C.
set -euo pipefail
cd "$(dirname "$0")/.."
if [ ! -f web/dist/index.html ]; then
  (cd web && npm ci --no-audit --no-fund && npm run build)
fi
go build -o remnantd ./cmd/remnantd
go build -o remnant-worker ./cmd/remnant-worker
exec ./remnantd local "$@"
