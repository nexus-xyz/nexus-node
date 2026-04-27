#!/bin/sh
set -e

HOME_DIR="${HOME_DIR:-/app}"

mkdir -p "${HOME_DIR}/data" "${HOME_DIR}/config"
chown nexus:nexus "${HOME_DIR}/config"
chown -R nexus:nexus "${HOME_DIR}/data"

exec su-exec nexus /usr/bin/nexusd start --home "${HOME_DIR}" "$@"
