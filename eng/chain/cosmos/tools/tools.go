//go:build tools

// Package tools declares tool dependencies for proto generation.
// These imports ensure `go mod tidy` keeps the tools in go.mod.
// See: https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package tools

import (
	_ "github.com/bufbuild/buf/cmd/buf"
	_ "github.com/cosmos/gogoproto/protoc-gen-gocosmos"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
	_ "github.com/cosmos/cosmos-proto/cmd/protoc-gen-go-pulsar"
	_ "github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway"
	_ "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2"
	_ "golang.org/x/tools/cmd/goimports"
)
