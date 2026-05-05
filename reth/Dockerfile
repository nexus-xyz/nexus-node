FROM lukemathwalker/cargo-chef:latest-rust-1.94.0 AS chef
WORKDIR /app

RUN apt-get update && apt-get install -y libclang-dev pkg-config protobuf-compiler

FROM chef AS planner
# Copy only the reth workspace for dependency analysis (no genesis files needed here).
COPY reth/ reth/
WORKDIR /app/reth
RUN cargo chef prepare --recipe-path recipe.json

FROM chef AS builder
WORKDIR /app/reth
COPY --from=planner /app/reth/recipe.json recipe.json

ARG BUILD_PROFILE=release
ENV BUILD_PROFILE=$BUILD_PROFILE

ARG RUSTFLAGS=""
ENV RUSTFLAGS="$RUSTFLAGS"

ARG FEATURES=""
ENV FEATURES=$FEATURES

RUN cargo chef cook --profile $BUILD_PROFILE --features "$FEATURES" --recipe-path recipe.json

# Copy the reth workspace and the cosmos chain-specs that are embedded via include_str!.
# include_str!("../../../cosmos/genesis/chain-specs/…") resolves relative to evm/src/ →
# three levels up lands at /app/, so cosmos/ must live at /app/cosmos/.
COPY reth/ .
COPY cosmos/genesis/chain-specs/ /app/cosmos/genesis/chain-specs/
RUN cargo build --profile $BUILD_PROFILE --features "$FEATURES" --locked --bin nexus-evm

# Copy to a known directory.
RUN if [ "$BUILD_PROFILE" = "dev" ]; then \
        cp /app/reth/target/debug/nexus-evm /app/nexus-evm; \
    else \
        cp /app/reth/target/release/nexus-evm /app/nexus-evm; \
    fi

FROM debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gosu \
    jq \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --system nexus && useradd --system --gid nexus --home-dir /app --create-home nexus

WORKDIR /app

COPY --from=builder /app/nexus-evm /usr/local/bin/nexus-evm
COPY reth/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 30303 30303/udp 9001 8545 8546

ENTRYPOINT ["docker-entrypoint.sh"]
