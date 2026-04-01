# syntax=docker.io/docker/dockerfile:1.7-labs

FROM lukemathwalker/cargo-chef:latest-rust-1.91.1 AS chef
WORKDIR /app

RUN apt-get update && apt-get install -y libclang-dev pkg-config protobuf-compiler

FROM chef AS planner
COPY . .
RUN cargo chef prepare --recipe-path recipe.json

FROM chef AS builder
COPY --from=planner /app/recipe.json recipe.json

ARG BUILD_PROFILE=release
ENV BUILD_PROFILE=$BUILD_PROFILE

ARG RUSTFLAGS=""
ENV RUSTFLAGS="$RUSTFLAGS"

ARG FEATURES=""
ENV FEATURES=$FEATURES

RUN cargo chef cook --profile $BUILD_PROFILE --features "$FEATURES" --recipe-path recipe.json

COPY . .
RUN cargo build --profile $BUILD_PROFILE --features "$FEATURES" --locked --bin nexus-evm

# Copy to a known directory.
RUN if [ "$BUILD_PROFILE" = "dev" ]; then \
        cp /app/target/debug/nexus-evm /app/nexus-evm; \
    else \
        cp /app/target/release/nexus-evm /app/nexus-evm; \
    fi

FROM debian
WORKDIR /app

COPY --from=builder /app/nexus-evm /usr/local/bin/nexus-evm

EXPOSE 30303 30303/udp 9001 8545 8546
ENTRYPOINT ["/usr/local/bin/nexus-evm"]
