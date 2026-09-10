#!/usr/bin/env bash

BRANCH=$(git symbolic-ref --short HEAD)
if [ -z ${GIT_COMMIT} ]; then GIT_COMMIT_ID=$(git rev-parse HEAD); fi
BUILD_TIME=$(date +"%Y-%m-%dT%H:%M:%S%z")
BUILD_INFO_FILE=build-info-data.js

cat <<EOF > ${BUILD_INFO_FILE}
module.exports = {
    branch: '${BRANCH}',
    version: '${GIT_COMMIT_ID}',
    build_time: '${BUILD_TIME}',
};
EOF
echo "Wrote ${BUILD_INFO_FILE}"
cat ${BUILD_INFO_FILE}