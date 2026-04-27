#!/usr/bin/env bash
set -euo pipefail

DATADIR="/data"

# Parse --datadir from args (handles --datadir=/path and --datadir /path)
args=("$@")
for i in "${!args[@]}"; do
  case "${args[$i]}" in
    --datadir=*) DATADIR="${args[$i]#*=}" ;;
    --datadir)   DATADIR="${args[$i+1]:-$DATADIR}" ;;
  esac
done

mkdir -p "$DATADIR"
chown -R nexus:nexus "$DATADIR"

exec gosu nexus /usr/local/bin/nexus-evm "$@"
