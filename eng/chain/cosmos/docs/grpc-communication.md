# gRPC Communication: L1 (Cosmos) and Core

This document describes how to run and test the bidirectional gRPC communication between the L1 (Cosmos) blockchain and
the Core coprocessor.

## Architecture Overview

```text
┌─────────────────────┐                    ┌─────────────────────┐
│     L1 (Cosmos)     │                    │   Core Coprocessor  │
│                     │                    │                     │
│  ┌───────────────┐  │    Cosmos → Core   │  ┌───────────────┐  │
│  │  CoreClient   │──┼───────────────────►│  │ CoreService   │  │
│  │  (outbound)   │  │    Port 50051      │  │ (NewPayload,  │  │
│  └───────────────┘  │                    │  │ FcUpd.)│  │
│                     │                    │  └───────────────┘  │
│  ┌───────────────┐  │    Core → Cosmos   │  ┌───────────────┐  │
│  │ CosmosService │◄─┼────────────────────┤  │  L1Client     │  │
│  │  (inbound)    │  │    Port 50052      │  │  (outbound)   │  │
│  │  (Ping)       │  │                    │  └───────────────┘  │
│  └───────────────┘  │                    │                     │
└─────────────────────┘                    └─────────────────────┘
```

**Communication Directions:**

- **Cosmos → Core (Port 50051)**: L1 sends block payloads and fork choice updates to Core for execution
- **Core → Cosmos (Port 50052)**: Core sends ping/health checks to verify L1 connectivity

## Prerequisites

1. **grpcurl** - Command-line gRPC client for testing

   ```bash
   # macOS
   brew install grpcurl

   # Linux
   go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
   ```

2. **Repositories**
   - L1: <https://github.com/nexus-xyz/l1>
   - Core: <https://github.com/nexus-xyz/core>

## Running the Servers

### 1. Start Core Coprocessor (on host)

The Core coprocessor runs on your host machine. The Reth containers connect to it at `host.docker.internal:50051`.

```bash
cd /path/to/core

# Build with gRPC and L1 client features
cargo build -p coprocessor --features grpc,l1_client

# Run with gRPC reflection enabled (for grpcurl discovery)
RUST_LOG=info cargo run -p coprocessor --features grpc,l1_client -- --enable-grpc-reflection
```

The Core server listens on `0.0.0.0:50051` by default.

**Environment Variables:**

| Variable    | Default | Description                   |
| ----------- | ------- | ----------------------------- |
| `GRPC_PORT` | `50051` | Port for the Core gRPC server |

### 2. Start L1 with Docker (Localnet)

The `l1-build/test-infrastructure` provides a Docker Compose setup for running a local multi-validator network.

**Directory Structure Required:**

```text
/your/workspace/
├── l1/                        # L1 repo
├── l1-build/
│   └── test-infrastructure/   # Docker Compose setup
└── core/                      # Core repo (for coprocessor)
```

**Docker Compose Configuration:**

The following changes are required in `docker-compose.yaml` to enable gRPC:

1. Add `GRPC_SERVER_ENABLED` to `x-cosmos-environment`:

   ```yaml
   x-cosmos-environment: &cosmos-environment # ... existing vars ...
     GRPC_SERVER_ENABLED: ${GRPC_SERVER_ENABLED:-false}
   ```

2. Expose port 50052 on `cosmos-validator-0`:

   ```yaml
   cosmos-validator-0:
     ports:
       - "26656:26656" # P2P
       - "26657:26657" # RPC
       - "26660:26660" # Prometheus
       - "50052:50052" # gRPC (for L1 ↔ Core communication)
   ```

**Running with gRPC enabled:**

```bash
# From the Nexus monorepo root:
cd eng/chain/test-infrastructure

# Use :local image tags in .env (default) so `just run` / `just start` builds via
# docker buildx bake when needed. To build the Cosmos image only:
#   docker buildx bake cosmos -f docker-bake.local.hcl --load

# First time: clean, setup, build (if :local), and run with gRPC enabled
just clean

COSMOS_IMAGE= docker compose build --no-cache cosmos-validator-0
GRPC_SERVER_ENABLED=true just run

# Subsequent runs (no rebuild needed)
GRPC_SERVER_ENABLED=true just start
```

**L1 Environment Variables:**

| Variable              | Default           | Description                     |
| --------------------- | ----------------- | ------------------------------- |
| `GRPC_SERVER_ENABLED` | `false`           | Enable the L1 gRPC server       |
| `COSMOS_GRPC_ADDR`    | `0.0.0.0:50052`   | Address for the L1 gRPC server  |
| `CORE_GRPC_ADDR`      | `localhost:50051` | Address of the Core gRPC server |

## Testing with grpcurl

### List Available Services

```bash
# Core services (port 50051)
grpcurl -plaintext localhost:50051 list

# Expected output:
# grpc.reflection.v1.ServerReflection
# nexus.Coprocessor
# nexus.TransactionService
# nexus.CoreService
# nexus.cosmos.v1.CosmosService

# L1 services (port 50052)
grpcurl -plaintext localhost:50052 list

# Expected output:
# grpc.reflection.v1alpha.ServerReflection
# nexus.cosmos.v1.CosmosService
```

### Test L1 Connectivity from Core

```bash
# CheckL1 - Core connects to L1 and pings it
grpcurl -plaintext -d '{"l1_addr": "http://localhost:50052"}' \
  localhost:50051 nexus.Coprocessor/CheckL1
```

### Describe Services

```bash
# Describe CoreService (Cosmos → Core)
grpcurl -plaintext localhost:50051 describe nexus.CoreService

# Describe CosmosService (Core → Cosmos)
grpcurl -plaintext localhost:50052 describe nexus.cosmos.v1.CosmosService
```
