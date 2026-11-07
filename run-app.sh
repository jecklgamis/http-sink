#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -f server.key ] || [ ! -f server.crt ]; then
    ./generate-ssl-certs.sh
fi

exec go run .
