#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
chmod +x go-bridge process-backend
exec ./go-bridge
