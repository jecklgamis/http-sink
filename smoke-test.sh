#!/usr/bin/env bash

GREEN='\033[0;32m'
RED='\033[0;31m'
NO_COLOR='\033[0m'

function ensure_get_ok() {
  url=$1
  status_code=$(curl -k --write-out '%{http_code}' --silent --output /dev/null ${url})
  if [[ "$status_code" -eq 200 ]] ; then
    echo -e "GET ${GREEN}${url} OK${NO_COLOR}"
  else
    echo -e "GET ${RED}${url} !OK${NO_COLOR}"
    exit 1
  fi
}

ensure_get_ok http://localhost:38080/
ensure_get_ok http://localhost:38080/build-info
ensure_get_ok http://localhost:38080/probe/ready
ensure_get_ok http://localhost:38080/probe/live
ensure_get_ok http://localhost:38080/blackhole
ensure_get_ok http://localhost:38080/blackhole/stats

ensure_get_ok https://localhost:8443/
ensure_get_ok https://localhost:8443/build-info
ensure_get_ok https://localhost:8443/probe/ready
ensure_get_ok https://localhost:8443/probe/live
ensure_get_ok https://localhost:8443/blackhole
ensure_get_ok https://localhost:8443/blackhole/stats
