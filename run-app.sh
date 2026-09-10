#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -d node_modules ]; then
    echo "Installing dependencies..."
    npm install
fi

if [ ! -f server.key ] || [ ! -f server.crt ]; then
    ./generate-ssl-certs.sh
fi

node app.js
