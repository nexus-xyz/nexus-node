# nexus (reth)

The Reth half of the Nexus chain. The `evm/` crate compiles to `nexus-evm`, the
execution-layer node: it ingests payloads from a consensus-layer client over
the Ethereum Engine API and executes them against an Ethereum-compatible state.

This directory is one half of a CL/EL split. The CL (`nexusd`, in the `cosmos/`
directory) owns consensus, governance, staking, IBC, and fee policy;
`nexus-evm` owns transaction execution and state. They communicate over
JWT-authenticated Engine API JSON-RPC.

## Layout

| Path                    | What lives here                                                                                  |
| ----------------------- | ------------------------------------------------------------------------------------------------ |
| `Cargo.toml`            | Workspace manifest — pins Reth `v1.10.2`, alloy/revm, and the rest of the EL toolchain.          |
| `evm/`                  | The `nexus-evm` binary: Reth node with a custom executor and Nexus-aware chain spec.             |
| `evm/src/main.rs`       | Entrypoint — wires `NexusChainSpecParser`, `NexusArgs`, and `NexusEthereumExecutorBuilder`.      |
| `evm/src/config.rs`     | `NexusEthEvmConfig` — wraps `EthEvmConfig` to swap in the custom block executor factory.         |
| `evm/src/evm.rs`        | `NexusEthEvmFactory` and `NexusEthereumExecutorBuilder` — the Reth `ExecutorBuilder` plug-in.    |
| `evm/src/block.rs`      | `NexusEthBlockExecutor` and `NexusEthBlockExecutorFactory` — block-execution wrapper with logs.  |
| `evm/src/forks.rs`      | `Nexus::V0` hardfork plus `NexusChainSpecParser` (reads Nexus extras out of genesis).            |
| `evm/src/nexus.rs`      | `NexusConfig` — YAML config struct with `fork_timings` for Prague, Osaka, and Nexus V0.          |
| `block-time-util/`      | The `nexus-block-time-util` binary — CLI for measuring block intervals over HTTP or WebSocket.   |
| `Dockerfile`            | cargo-chef multistage build that produces a `debian:trixie-slim` image with `nexus-evm`.         |
| `docker-entrypoint.sh`  | Runtime entrypoint — parses `--datadir`, fixes ownership, and execs `nexus-evm` as the `nexus` user. |

## Engine API ↔ Reth wiring

`nexus-evm` is a Reth node assembled through Reth's `NodeBuilder`, with two
Nexus-specific seams:

- **Custom executor** — `NexusEthereumExecutorBuilder` (`evm/src/evm.rs`)
  swaps Reth's default `EthEvmConfig` for `NexusEthEvmConfig`. The wrapper
  delegates the EVM environment, block assembly, and Engine API payload paths
  (`ConfigureEvm`, `ConfigureEngineEvm<ExecutionData>`) to the inner Eth
  config, but installs `NexusEthBlockExecutorFactory` so every block goes
  through `NexusEthBlockExecutor` for structured `nexus::block` tracing on
  pre-execution, per-transaction, and finalize hooks.
- **Custom chain spec** — `NexusChainSpecParser` (`evm/src/forks.rs`) parses
  genesis and pulls `v0_time` out of the `config.extra_fields` block,
  registering the Nexus-defined `Nexus::V0` hardfork. `NexusConfig::apply_forks`
  (`evm/src/nexus.rs`) layers timestamp-based activations for `Prague`,
  `Osaka`, and `Nexus::V0` over the parsed chain spec at startup.

Engine API version selection (V3, V4 Prague, V5 Osaka) is driven by the EL
fork schedule. Keep `fork_timings.prague_time` / `osaka_time` here aligned
with the CL's `forks.prague_timestamp` / `forks.osaka_timestamp` so the two
halves negotiate the same Engine API tier per payload.

## Build & run

Requires Rust 1.94 (matches the Dockerfile toolchain and `rust-toolchain.toml`)
and the system dependencies installed by the Docker build: `libclang-dev`,
`pkg-config`, and `protobuf-compiler`.

```bash
cargo build --release --bin nexus-evm        # builds the EL binary
cargo run -p nexus-evm -- --help             # node CLI (Reth flags + --nexus-config)
cargo test --workspace                       # unit tests
cargo clippy --workspace --all-targets       # lints
```

The Docker image builds `nexus-evm` directly via `cargo build` against a
cargo-chef recipe. See `Dockerfile` and `docker-entrypoint.sh`. Default
exposed ports: `30303/tcp`, `30303/udp` (P2P), `9001` (metrics), `8545`
(JSON-RPC HTTP), `8546` (JSON-RPC WS).

## Configuration

Pass `--nexus-config <path>` to `nexus-evm` to load a YAML file shaped by
`NexusConfig` (`evm/src/nexus.rs`). All fields are optional; omit a fork
timestamp to leave the chain spec's default activation in place.

```yaml
fork_timings:
  prague_time: 1735689600   # ETH Prague (Engine API V4) activation
  osaka_time:  1767225600   # ETH Osaka (Engine API V5) activation
  v0_time:     1735689600   # Nexus V0 activation
```

`NexusConfig::parse_nexus_config` validates the file at parse time; among
other checks, `osaka_time` must be strictly greater than `prague_time` when
both are set.

Genesis-level Nexus parameters (currently `v0_time`) can also be embedded in
the chain's genesis JSON under `config.extra_fields`; the parser picks them up
through `NexusHardforkConfig`.

## Block-time utility

`block-time-util/` is a small operational CLI for sanity-checking RPC
endpoints. It supports HTTP polling and WebSocket subscriptions, and a
`--smoke` mode that confirms WSS upgrade and `eth_subscribe` framing in one
shot. See [`block-time-util/README.md`](./block-time-util/README.md) for
usage.

## Verifying release images

Container images are signed by the build workflow with cosign. Verify before
pulling into any trusted environment:

```bash
cosign verify \
  --certificate-identity-regexp="https://github.com/nexus-xyz/nexus/.github/workflows/eng-chain-reth-build.yml@refs/.*" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  nexusxyz/reth:<tag> | jq .
```

## Releases

Tag a commit with a `reth/v`-prefixed semver tag and push it. The release
workflow picks up the tag, builds the image (verifying first that the tag
points to a commit on `main`), pushes it to `nexusxyz/reth`, and signs it
with cosign.

```bash
git tag reth/v0.1.0
git push origin reth/v0.1.0
```
