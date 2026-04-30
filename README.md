# Nexus Node

This repository contains the source code for the two binaries that make up a Nexus node:

- `reth/` — the EVM execution client (`nexus-evm`)
- `cosmos/` — the consensus client (`nexusd`)

This guide walks through building and running a **fullnode** directly from source.

> Validator and archivenode setups follow the same shape but require additional steps
> (key generation, pruning configuration). The binary invocations below carry over
> directly; only the flags and node-type environment variables differ.

## Architecture

A Nexus node is two cooperating processes on the same host:

```
┌─────────────────────────────────────────────┐
│  nexusd          (consensus client)         │
│  CometBFT P2P:   26656                      │
│  CometBFT RPC:   26657                      │
│         │                                   │
│         │ Engine API (JWT-authenticated)    │
│         ▼                                   │
│  nexus-evm       (EVM execution client)     │
│  HTTP JSON-RPC:  8545                       │
│  WebSocket RPC:  8546                       │
│  Engine API:     8551 (loopback only)       │
│  EVM P2P:        30303                      │
└─────────────────────────────────────────────┘
```

`nexusd` proposes and validates blocks; for each block it calls `nexus-evm` over the Engine
API to execute transactions and update EVM state. The Engine API is authenticated with a
shared JWT secret that both processes read from disk at startup.

Port `8551` must never be reachable outside the host.

## Networks

| Network | Chain ID |
|---|---|
| devnet  | `3940` |
| testnet | `3945` |
| mainnet | `3946` |

The rest of this guide uses `CHAIN_ID=3946` (mainnet). Substitute the chain ID for the
network you intend to join.

## Prerequisites

| Tool | Version | Purpose |
|---|---|---|
| Rust toolchain | 1.91+ (`rustup`) | Build `nexus-evm` |
| Go | 1.25+ | Build `nexusd` |
| `clang` / `libclang`, `pkg-config`, `protobuf-compiler` | — | Native deps for `nexus-evm` |
| `openssl` | any | Generate the Engine API JWT secret |
| Google Cloud SDK (`gcloud`) | any | Pull chain configuration (genesis, peers) from `gs://nexus-l1` |

### Hardware

Same as the container deployment:

- 8 vCPUs, 32 GB RAM
- Fullnode: 500 GB NVMe / SSD (spinning disk is not supported)
- The EVM and consensus data directories are I/O-heavy — keep them on fast local storage.

## Quick start: `setup.sh`

This repository ships an interactive `setup.sh` that performs every step in this guide
(steps 1–5 below) in order, with a `[Y/n/s]` prompt before each one. After it finishes,
it prints the exact `nexus-evm` and `nexusd` commands to start the node (steps 6–7).

```bash
./setup.sh
```

By default it targets mainnet and places node data in `./node/`. Override either by
creating a `.env` next to the script:

```
CHAIN_ID=3945
ENVIRONMENT=testnet
NODE_HOME=/var/lib/nexus
```

Re-running `setup.sh` is safe: existing builds, the JWT secret, and an already-initialised
cosmos home are preserved. The remaining sections describe the same steps performed
manually, in the same order — read them if you want to understand or customise what the
script does.

## 1. Build the binaries

From the root of this repository:

```bash
# EVM execution client → ./reth/target/release/nexus-evm
cd reth
cargo build --release --locked --bin nexus-evm
cd ..

# Consensus client → $(go env GOPATH)/bin/nexusd
cd cosmos
make install
cd ..
```

Confirm both binaries are on your `PATH`:

```bash
nexus-evm --version
nexusd version
```

(If `nexus-evm` isn't on your `PATH`, either copy it from `reth/target/release/nexus-evm`
or invoke it by absolute path in the commands below.)

## 2. Lay out the node directory

Pick a directory to hold all node state. The rest of the guide assumes:

```bash
export NODE_HOME="$HOME/nexus-node-data"
mkdir -p "$NODE_HOME"/{config/cosmos,config/evm,jwt,reth-data}
```

Final layout (after the steps that follow):

```
$NODE_HOME/
├── config/
│   ├── cosmos/
│   │   ├── genesis.json                  # from GCS
│   │   ├── nexus-config.cosmos.yaml      # from GCS
│   │   ├── config.toml                   # CometBFT config (peer-substituted)
│   │   └── app.toml                      # Cosmos SDK app config
│   └── evm/
│       ├── genesis.json                  # from GCS
│       └── nexus-config.evm.yaml         # from GCS
├── jwt/jwt.hex                           # shared Engine API secret
├── reth-data/                            # nexus-evm chain state
└── cosmos-home/                          # nexusd home (data + config + keys)
```

## 3. Download the chain configuration

The genesis files, the Nexus-specific configs, and the canonical peer/version list for each
network all live under `gs://nexus-l1/chain/<CHAIN_ID>/`:

```bash
export CHAIN_ID=3946
gcloud storage cp -r "gs://nexus-l1/chain/${CHAIN_ID}/*" "$NODE_HOME/config/"
```

This produces `config/.env`, `config/cosmos/{genesis.json,nexus-config.cosmos.yaml}`, and
`config/evm/{genesis.json,nexus-config.evm.yaml}`.

Load the peer/version values from the network `.env` into your shell:

```bash
set -a; . "$NODE_HOME/config/.env"; set +a
echo "$PERSISTENT_PEERS"   # CometBFT P2P peers — nodeid@host:port,...
echo "$RPC_SERVERS"        # CometBFT RPC endpoints
echo "$TRUSTED_PEERS"      # EVM enode:// peers
```

You only need `PERSISTENT_PEERS`, `RPC_SERVERS`, and `TRUSTED_PEERS` from this file —
the `*_IMAGE` variables are for the container deployment and can be ignored.

## 4. Generate the JWT secret

```bash
openssl rand -hex 32 > "$NODE_HOME/jwt/jwt.hex"
chmod 600 "$NODE_HOME/jwt/jwt.hex"
```

Both binaries read this file. Generate it once and keep it stable across restarts.

## 5. Initialise `nexusd`

`nexusd init` creates a CometBFT home directory with a fresh `node_key.json`,
`config.toml`, and `app.toml`. We then overwrite the genesis and config files with the
network configs from GCS.

```bash
export COSMOS_HOME="$NODE_HOME/cosmos-home"
nexusd init nexus-fullnode --chain-id "${CHAIN_ID}" --home "$COSMOS_HOME"

# Replace the generated genesis with the network genesis
cp "$NODE_HOME/config/cosmos/genesis.json" "$COSMOS_HOME/config/genesis.json"

# Drop in the Nexus consensus config
cp "$NODE_HOME/config/cosmos/nexus-config.cosmos.yaml" "$COSMOS_HOME/config/nexus-config.cosmos.yaml"
```

Set the peer lists in `$COSMOS_HOME/config/config.toml`. The simplest approach is `sed`:

```bash
sed -i.bak \
  -e "s|^persistent_peers = .*|persistent_peers = \"${PERSISTENT_PEERS}\"|" \
  -e "s|^seeds = .*|seeds = \"\"|" \
  "$COSMOS_HOME/config/config.toml"
rm "$COSMOS_HOME/config/config.toml.bak"
```

If you intend to use state sync, also point `[statesync]` `rpc_servers` at `$RPC_SERVERS`
and supply a `trust_height` / `trust_hash`. State sync is **off by default** in the
network config and is fine to leave off — block sync from height 0 is fast on this chain.

The default `pruning = "default"` in `app.toml` is correct for a fullnode. Set it to
`"nothing"` if you want an archivenode.

> `nexusd init` also writes a `priv_validator_key.json`. A fullnode does not register
> this key on-chain, so it never signs blocks — it is harmless. Delete it if you prefer,
> or back it up if you might promote this node to a validator later.

## 6. Run `nexus-evm`

`nexus-evm` must be running and serving the Engine API before `nexusd` is started.

```bash
nexus-evm node \
  --datadir="$NODE_HOME/reth-data" \
  --chain="$NODE_HOME/config/evm/genesis.json" \
  --nexus-config="$NODE_HOME/config/evm/nexus-config.evm.yaml" \
  --authrpc.jwtsecret="$NODE_HOME/jwt/jwt.hex" \
  --authrpc.addr=127.0.0.1 \
  --authrpc.port=8551 \
  --http --http.addr=0.0.0.0 --http.port=8545 --http.corsdomain='*' \
  --http.api=eth,net,web3,debug,txpool \
  --ws  --ws.addr=0.0.0.0  --ws.port=8546  --ws.origins='*' \
  --ws.api=eth,net,web3,debug,txpool \
  --trusted-peers="${TRUSTED_PEERS}" \
  --metrics=0.0.0.0:9001 \
  --txpool.lifetime=30s \
  --txpool.disable-transactions-backup \
  --txpool.nolocals \
  --engine.persistence-threshold=0 \
  --engine.memory-block-buffer-target=0 \
  --full
```

Notes:

- `--full` is the fullnode flag. Omit it for an archivenode and add `trace` to the
  `--http.api` and `--ws.api` lists.
- `--authrpc.addr=127.0.0.1` keeps the Engine API on loopback. Do not bind it to a
  public address.
- `--engine.persistence-threshold=0` and `--engine.memory-block-buffer-target=0` match
  the production configuration used by Nexus.

## 7. Run `nexusd`

In a second shell, with `nexus-evm` already up:

```bash
export EVM_ENGINE_JWT_SECRET_PATH="$NODE_HOME/jwt/jwt.hex"
export EVM_ENGINE_URL="http://127.0.0.1:8551"
export NEXUS_CONFIG_PATH="$COSMOS_HOME/config/nexus-config.cosmos.yaml"

nexusd start --home "$COSMOS_HOME"
```

These three environment variables are the contract between `nexusd` and `nexus-evm`:

| Variable | Purpose |
|---|---|
| `EVM_ENGINE_URL` | Engine API endpoint of the local `nexus-evm` |
| `EVM_ENGINE_JWT_SECRET_PATH` | Path to the shared JWT secret (must match `nexus-evm --authrpc.jwtsecret`) |
| `NEXUS_CONFIG_PATH` | Nexus-specific consensus config (`nexus-config.cosmos.yaml`) — required at startup |

For unattended operation, run each binary under `systemd`, `supervisord`, or your
process manager of choice. The consensus client must be started **after** `nexus-evm` is
serving the Engine API.

## 8. Verify

```bash
# CometBFT status — should report sync progress and the chain ID
curl -s http://localhost:26657/status | jq '.result.sync_info'

# EVM JSON-RPC — should return a block number that climbs while syncing
curl -s -X POST -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}' \
  http://localhost:8545 | jq
```

Once `catching_up` flips to `false` in the CometBFT status, the node is at the chain tip.

## Ports

| Port | Protocol | Process | Expose publicly? | Description |
|---|---|---|---|---|
| `8545`  | TCP | nexus-evm | Yes | HTTP JSON-RPC |
| `8546`  | TCP | nexus-evm | Yes | WebSocket JSON-RPC |
| `8551`  | TCP | nexus-evm | **No** | Engine API — loopback only |
| `30303` | TCP+UDP | nexus-evm | Yes | EVM P2P |
| `9001`  | TCP | nexus-evm | Optional | Prometheus metrics |
| `26656` | TCP | nexusd    | Yes | CometBFT P2P |
| `26657` | TCP | nexusd    | Yes | CometBFT RPC |

## Updating

Network upgrades typically require new binary versions and may publish updated configs to
GCS. To update:

1. Stop `nexusd`, then `nexus-evm`.
2. Pull the latest source for this repository and rebuild both binaries (step 1).
3. Re-download `gs://nexus-l1/chain/<CHAIN_ID>/` to refresh `genesis.json`,
   `nexus-config.*.yaml`, and the peer lists in `config/.env`.
4. Re-apply the peer substitutions in `$COSMOS_HOME/config/config.toml` if peers rotated.
5. Start `nexus-evm`, then `nexusd`.

The JWT secret, the `reth-data` directory, and the CometBFT data directory persist across
restarts and upgrades — do not delete them.

## Published images

Official container images are published to Docker Hub as `nexusxyz/reth` and
`nexusxyz/cosmos`. See the release notes on each tag for image-signing details.

## Source layout

| Path | Description |
|---|---|
| `reth/evm/`             | `nexus-evm` binary — Reth-based EVM execution client with Nexus customizations |
| `reth/block-time-util/` | Helper crate used by the EVM client |
| `cosmos/cmd/nexusd/`    | `nexusd` binary entry point |
| `cosmos/app/`           | Cosmos SDK app wiring |
| `cosmos/x/`             | Nexus-specific Cosmos modules (including `x/evm` — the Engine API bridge) |
| `cosmos/proto/`         | Protobuf definitions |
| `cosmos/lib/`           | Shared helpers (config loading, engine client) |

## Support

For onboarding support, contact the Nexus team.
