FROM golang:1.25.8-alpine3.22 AS build-env

RUN apk add --no-cache \
    ca-certificates \
    build-base \
    git \
    bash \
    curl

WORKDIR /workspace

# Build deps first
COPY go.* ./
RUN go mod tidy

# Copy source code
COPY . ./

# Build the binary manually (no ignite)
RUN go build -o /usr/bin/nexusd ./cmd/nexusd

# Final runtime image
FROM alpine:3.22

WORKDIR /root

RUN apk add --no-cache \
    ca-certificates \
    curl \
    sed

# Copy binary
COPY --from=build-env /usr/bin/nexusd /usr/bin/nexusd

WORKDIR /home/l1

ENV HOME_DIR=/home/l1

EXPOSE 26656 26657 1317 9090

HEALTHCHECK CMD curl --fail http://localhost:26657 || exit 1

ENTRYPOINT ["nexusd", "start"]
CMD ["--home", "/home/l1"]
