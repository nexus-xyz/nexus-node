# block-time-util

CLI to watch EVM block arrival intervals over **HTTP** (polling) or **WebSocket** (subscriptions).

## WebSocket smoke check (testnet)

Use this before mainnet launch to confirm **WSS upgrade** and **JSON-RPC** subscriptions (`eth_subscribe` / new heads) against the hosted RPC.

From `reth/`:

```bash
cargo run -p nexus-block-time-util -- --smoke --url 'wss://testnet.rpc.nexus.xyz'
```

Success prints a line including the first observed block number and exits `0`. Any TLS, upgrade, framing, or subscription failure exits non-zero with an error. By default the first `newHeads` event must arrive within **60 seconds** (override with `--smoke-timeout-secs`, range 1–86400).

If you still see `403 Forbidden` on the WebSocket handshake while HTTPS JSON-RPC works, the testnet Envoy config may not be synced yet (ArgoCD) or another edge policy (geo, rate limit) may apply.

For a long-running view of block intervals (no `--smoke`):

```bash
cargo run -p nexus-block-time-util -- --url 'wss://testnet.rpc.nexus.xyz'
```

Environment: same flags support `RPC_URL` instead of `--url` when set.
