#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# setup.sh — Interactive bootstrapper for a Nexus fullnode built from source.
#
# Walks through every step in the README:
#   1. Verify required tooling
#   2. Confirm network (chain ID / environment)
#   3. Build nexus-evm and nexusd
#   4. Lay out the node directory
#   5. Download chain configs from GCS
#   6. Generate the JWT secret
#   7. Initialise nexusd and inject network configs/peers
#   8. Print the commands to start the node
#
# Defaults to mainnet. Override by creating a .env in this directory:
#   CHAIN_ID=3945
#   ENVIRONMENT=testnet
#   NODE_HOME=/var/lib/nexus      # optional — where node data lives
#
# Re-running is safe: each step is idempotent, and any step can be skipped
# at the prompt.
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# ---------------------------------------------------------------------------
# Pretty printing
# ---------------------------------------------------------------------------
RED=$'\033[1;31m'
GREEN=$'\033[1;32m'
YELLOW=$'\033[1;33m'
CYAN=$'\033[1;36m'
BOLD=$'\033[1m'
DIM=$'\033[2m'
RESET=$'\033[0m'

step()  { printf '\n%s==>%s %s%s%s\n' "$CYAN" "$RESET" "$BOLD" "$1" "$RESET"; }
info()  { printf '    %s\n' "$1"; }
warn()  { printf '%s!%s   %s\n' "$YELLOW" "$RESET" "$1"; }
ok()    { printf '%s✔%s   %s\n' "$GREEN" "$RESET" "$1"; }
fail()  { printf '%s✘%s   %s\n' "$RED" "$RESET" "$1" >&2; exit 1; }

# Prompt for [Y/n/s] — yes / no (abort) / skip this step.
confirm() {
  local prompt="$1"
  while :; do
    printf '%s?%s   %s [Y/n/s] ' "$YELLOW" "$RESET" "$prompt"
    read -r reply </dev/tty || reply="y"
    case "${reply:-y}" in
      y|Y|yes|YES) return 0 ;;
      n|N|no|NO)   fail "Aborted by user." ;;
      s|S|skip)    return 1 ;;
      *) echo "    Please answer y, n, or s." ;;
    esac
  done
}

# ---------------------------------------------------------------------------
# Defaults — mainnet unless overridden by a local .env or environment
# ---------------------------------------------------------------------------
CHAIN_ID=3946
ENVIRONMENT=mainnet
NODE_HOME="${SCRIPT_DIR}/node"

if [ -f .env ]; then
  set -a; . ./.env; set +a
fi

# Recompute paths after .env is sourced
NODE_HOME="${NODE_HOME:-${SCRIPT_DIR}/node}"
COSMOS_HOME="${NODE_HOME}/cosmos-home"
RETH_DATA="${NODE_HOME}/reth-data"
JWT_FILE="${NODE_HOME}/jwt/jwt.hex"
CONFIG_DIR="${NODE_HOME}/config"
GCS_PATH="gs://nexus-l1/chain/${CHAIN_ID}"

RETH_DIR="${SCRIPT_DIR}/reth"
COSMOS_DIR="${SCRIPT_DIR}/cosmos"
RETH_BIN="${RETH_DIR}/target/release/nexus-evm"

# ---------------------------------------------------------------------------
# Banner
# ---------------------------------------------------------------------------
cat <<EOF

${BOLD}Nexus Node — interactive setup${RESET}
${DIM}-------------------------------------------------------${RESET}
  Network    : ${CYAN}${ENVIRONMENT} (chain ID ${CHAIN_ID})${RESET}
  Node home  : ${CYAN}${NODE_HOME}${RESET}
  Sources    : ${CYAN}${RETH_DIR}${RESET}
               ${CYAN}${COSMOS_DIR}${RESET}

  Override defaults by creating a .env in this directory:
    CHAIN_ID=3945
    ENVIRONMENT=testnet
    NODE_HOME=/var/lib/nexus

  At each prompt:  ${BOLD}y${RESET} = run this step,  ${BOLD}n${RESET} = abort,  ${BOLD}s${RESET} = skip.
${DIM}-------------------------------------------------------${RESET}
EOF

confirm "Proceed with these settings?" || fail "Aborted."

# ---------------------------------------------------------------------------
# 1. Tooling check
# ---------------------------------------------------------------------------
step "Step 1/7 — Check required tooling"

require_cmd() {
  local cmd="$1" purpose="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    ok "$(printf '%-10s — %s' "$cmd" "$($cmd --version 2>&1 | head -n1)")"
  else
    fail "$cmd not found ($purpose). Install it and re-run."
  fi
}

require_cmd cargo   "build nexus-evm"
require_cmd rustc   "build nexus-evm"
require_cmd go      "build nexusd"
require_cmd openssl "generate JWT secret"
require_cmd gcloud  "download chain configs from gs://nexus-l1"
require_cmd sed     "edit config.toml"
require_cmd curl    "verify the running node (optional but recommended)"

# Confirm gcloud is authenticated for the chain config bucket
if ! gcloud auth list --filter=status:ACTIVE --format='value(account)' 2>/dev/null | grep -q .; then
  warn "gcloud has no active account — run: gcloud auth login"
  confirm "Continue anyway? (downloads will fail without auth)" || fail "Aborted."
else
  ok "gcloud authenticated as $(gcloud auth list --filter=status:ACTIVE --format='value(account)' | head -n1)"
fi

# ---------------------------------------------------------------------------
# 2. Build nexus-evm
# ---------------------------------------------------------------------------
step "Step 2/7 — Build nexus-evm (Rust, release profile)"
info "Builds in: ${RETH_DIR}"
info "Output:    ${RETH_BIN}"

if confirm "Build nexus-evm now?"; then
  if [ -x "${RETH_BIN}" ]; then
    warn "Existing binary found at ${RETH_BIN}; cargo will rebuild only what changed."
  fi
  ( cd "${RETH_DIR}" && cargo build --release --bin nexus-evm )
  [ -x "${RETH_BIN}" ] || fail "Build finished but ${RETH_BIN} is missing."
  ok "Built ${RETH_BIN}"
else
  [ -x "${RETH_BIN}" ] || warn "Skipped — but ${RETH_BIN} does not exist yet. Step 8 will fail until you build it."
fi

# ---------------------------------------------------------------------------
# 3. Build nexusd
# ---------------------------------------------------------------------------
step "Step 3/7 — Build nexusd (Go, via 'make install')"
GOBIN="$(go env GOBIN)"
[ -n "$GOBIN" ] || GOBIN="$(go env GOPATH)/bin"
info "Installs to: ${GOBIN}/nexusd"

if confirm "Build nexusd now?"; then
  ( cd "${COSMOS_DIR}" && make install )
  # Don't invoke nexusd here — every subcommand instantiates the app, which
  # tries to read jwt.hex on construction and panics until step 6 runs.
  # Verify by checking the binary exists instead.
  if [ ! -x "${GOBIN}/nexusd" ]; then
    fail "make install finished but nexusd is not at ${GOBIN}/nexusd."
  fi
  if ! command -v nexusd >/dev/null 2>&1; then
    warn "${GOBIN} is not on your PATH. Add it before starting the node:"
    info "  export PATH=\"${GOBIN}:\$PATH\""
  fi
  ok "Built ${GOBIN}/nexusd"
else
  command -v nexusd >/dev/null 2>&1 || warn "Skipped — nexusd not found on PATH. Step 7 will fail until you build it."
fi

# ---------------------------------------------------------------------------
# 4. Lay out the node directory
# ---------------------------------------------------------------------------
step "Step 4/7 — Create node directory layout"
info "Creating: ${NODE_HOME}/{config/cosmos,config/evm,jwt,reth-data}"

if confirm "Create directories under ${NODE_HOME}?"; then
  mkdir -p "${CONFIG_DIR}/cosmos" "${CONFIG_DIR}/evm" "${NODE_HOME}/jwt" "${RETH_DATA}"
  ok "Directory layout ready."
fi

# ---------------------------------------------------------------------------
# 5. Download chain configs from GCS
# ---------------------------------------------------------------------------
step "Step 5/7 — Download chain configs"
info "Source: ${GCS_PATH}/"
info "Dest:   ${CONFIG_DIR}/"

if confirm "Download chain configs from GCS?"; then
  gcloud storage cp -r "${GCS_PATH}/*" "${CONFIG_DIR}/"
  ok "Downloaded chain configs."
fi

# Load network defaults from the downloaded .env (peers, etc.). Local env wins.
NETWORK_ENV="${CONFIG_DIR}/.env"
if [ -f "${NETWORK_ENV}" ]; then
  info "Loading peer/version defaults from ${NETWORK_ENV}"
  while IFS= read -r line || [ -n "$line" ]; do
    [[ "$line" =~ ^#.*$ || -z "$line" ]] && continue
    var="${line%%=*}"
    val="${line#*=}"
    if [ -z "${!var:-}" ]; then export "$var"="$val"; fi
  done < "${NETWORK_ENV}"
fi

# Soft validation — only required for the steps that follow
PEERS_OK=true
for v in PERSISTENT_PEERS RPC_SERVERS TRUSTED_PEERS; do
  if [ -z "${!v:-}" ]; then
    warn "${v} is not set (expected from ${NETWORK_ENV})."
    PEERS_OK=false
  fi
done
$PEERS_OK && ok "Peer lists loaded (PERSISTENT_PEERS, RPC_SERVERS, TRUSTED_PEERS)."

# ---------------------------------------------------------------------------
# 6. Generate JWT secret
# ---------------------------------------------------------------------------
step "Step 6/7 — Generate JWT secret for the Engine API"
info "File: ${JWT_FILE}"

if [ -f "${JWT_FILE}" ]; then
  ok "JWT secret already exists — preserved across runs."
else
  if confirm "Generate a new 32-byte JWT secret?"; then
    mkdir -p "${NODE_HOME}/jwt"
    openssl rand -hex 32 > "${JWT_FILE}"
    chmod 600 "${JWT_FILE}"
    ok "Wrote ${JWT_FILE}"
  fi
fi

# ---------------------------------------------------------------------------
# 7. Initialise nexusd and inject network configs / peers
# ---------------------------------------------------------------------------
step "Step 7/7 — Initialise nexusd and apply network configs"
info "Cosmos home: ${COSMOS_HOME}"

if confirm "Run 'nexusd init' and apply chain configs?"; then
  if ! command -v nexusd >/dev/null 2>&1; then
    GOBIN="$(go env GOBIN)"; [ -n "$GOBIN" ] || GOBIN="$(go env GOPATH)/bin"
    if [ -x "${GOBIN}/nexusd" ]; then
      export PATH="${GOBIN}:${PATH}"
    else
      fail "nexusd not on PATH and not at ${GOBIN}/nexusd. Build it first (step 3)."
    fi
  fi

  # Every nexusd subcommand instantiates the app, which loads the JWT secret on
  # construction. Point it at our generated secret so 'init' doesn't panic.
  export EVM_ENGINE_JWT_SECRET_PATH="${JWT_FILE}"

  if [ -f "${COSMOS_HOME}/config/genesis.json" ]; then
    warn "${COSMOS_HOME} already initialised — skipping 'nexusd init'."
  else
    nexusd init nexus-fullnode --chain-id "${CHAIN_ID}" --home "${COSMOS_HOME}"
    ok "nexusd init complete."
  fi

  # Always overwrite genesis + nexus consensus config from the downloaded files
  cp "${CONFIG_DIR}/cosmos/genesis.json"             "${COSMOS_HOME}/config/genesis.json"
  cp "${CONFIG_DIR}/cosmos/nexus-config.cosmos.yaml" "${COSMOS_HOME}/config/nexus-config.cosmos.yaml"
  ok "Installed network genesis.json and nexus-config.cosmos.yaml."

  # Substitute peers in config.toml — only if both vars are set
  if [ -n "${PERSISTENT_PEERS:-}" ]; then
    sed -i.bak \
      -e "s|^persistent_peers = .*|persistent_peers = \"${PERSISTENT_PEERS}\"|" \
      -e "s|^seeds = .*|seeds = \"\"|" \
      "${COSMOS_HOME}/config/config.toml"
    rm -f "${COSMOS_HOME}/config/config.toml.bak"
    ok "Wrote persistent_peers into ${COSMOS_HOME}/config/config.toml"
  else
    warn "PERSISTENT_PEERS not set — edit ${COSMOS_HOME}/config/config.toml manually before starting."
  fi
fi

# ---------------------------------------------------------------------------
# Done — print the start commands
# ---------------------------------------------------------------------------
GOBIN="$(go env GOBIN 2>/dev/null || true)"; [ -n "$GOBIN" ] || GOBIN="$(go env GOPATH 2>/dev/null)/bin"

cat <<EOF

${GREEN}${BOLD}✔ Setup complete.${RESET}

${BOLD}Start the node in two shells, in this order:${RESET}

${BOLD}Shell 1 — nexus-evm (EVM execution client)${RESET}
  ${RETH_BIN} node \\
    --datadir="${RETH_DATA}" \\
    --chain="${CONFIG_DIR}/evm/genesis.json" \\
    --nexus-config="${CONFIG_DIR}/evm/nexus-config.evm.yaml" \\
    --authrpc.jwtsecret="${JWT_FILE}" \\
    --authrpc.addr=127.0.0.1 --authrpc.port=8551 \\
    --http --http.addr=0.0.0.0 --http.port=8545 --http.corsdomain='*' \\
    --http.api=eth,net,web3,debug,txpool \\
    --ws --ws.addr=0.0.0.0 --ws.port=8546 --ws.origins='*' \\
    --ws.api=eth,net,web3,debug,txpool \\
    --trusted-peers="${TRUSTED_PEERS:-<TRUSTED_PEERS>}" \\
    --metrics=0.0.0.0:9001 \\
    --txpool.lifetime=30s --txpool.disable-transactions-backup --txpool.nolocals \\
    --engine.persistence-threshold=0 --engine.memory-block-buffer-target=0 \\
    --full

${BOLD}Shell 2 — nexusd (consensus client)${RESET}
  export EVM_ENGINE_JWT_SECRET_PATH="${JWT_FILE}"
  export EVM_ENGINE_URL="http://127.0.0.1:8551"
  export NEXUS_CONFIG_PATH="${COSMOS_HOME}/config/nexus-config.cosmos.yaml"
  ${GOBIN}/nexusd start --home "${COSMOS_HOME}"

${BOLD}Endpoints once running:${RESET}
  EVM HTTP RPC : http://localhost:8545
  EVM WS RPC   : ws://localhost:8546
  CometBFT RPC : http://localhost:26657

${BOLD}Verify:${RESET}
  curl -s http://localhost:26657/status | jq '.result.sync_info'

EOF
