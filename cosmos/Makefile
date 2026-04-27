BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
COMMIT := $(shell git log -1 --format='%H')
APPNAME := nexus

# do not override user values
ifeq (,$(VERSION))
  VERSION := $(shell git describe --exact-match 2>/dev/null)
  # if VERSION is empty, then populate it with branch name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

# Update the ldflags with the app, client & server names
ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=$(APPNAME) \
	-X github.com/cosmos/cosmos-sdk/version.AppName=$(APPNAME)d \
	-X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

BUILD_FLAGS := -ldflags '$(ldflags)'

##############
###  Test  ###
##############

test-unit:
	@echo Running unit tests...
	@go test -mod=readonly -v -timeout 30m ./...

test-race:
	@echo Running unit tests with race condition reporting...
	@go test -mod=readonly -v -race -timeout 30m ./...

test-cover:
	@echo Running unit tests and creating coverage report...
	@go test -mod=readonly -v -timeout 30m -coverprofile=$(COVER_FILE) -covermode=atomic ./...
	@go tool cover -html=$(COVER_FILE) -o $(COVER_HTML_FILE)
	@rm $(COVER_FILE)

bench:
	@echo Running unit tests with benchmarking...
	@go test -mod=readonly -v -timeout 30m -bench=. ./...

fuzz:
	@echo "Running fuzzing tests..."
	@echo "Discovering and running all fuzz tests with 10s timeout..."
	@for dir in $$(find . -name "*.go" -exec grep -l "func Fuzz" {} \; | xargs dirname | sort -u); do \
		echo "Checking $$dir for fuzz tests..."; \
		for fuzz in $$(go test -list=Fuzz $$dir 2>/dev/null | grep "^Fuzz"); do \
			echo "  Running $$fuzz in $$dir..."; \
			go test -fuzz=^$$fuzz$$ -fuzztime=10s $$dir || true; \
		done; \
	done

fuzz-long:
	@echo "Running long fuzzing tests..."
	@echo "Discovering and running all fuzz tests with 30s timeout..."
	@for dir in $$(find . -name "*.go" -exec grep -l "func Fuzz" {} \; | xargs dirname | sort -u); do \
		echo "Checking $$dir for fuzz tests..."; \
		for fuzz in $$(go test -list=Fuzz $$dir 2>/dev/null | grep "^Fuzz"); do \
			echo "  Running $$fuzz in $$dir..."; \
			go test -fuzz=^$$fuzz$$ -fuzztime=30s $$dir || true; \
		done; \
	done


test: govet govulncheck test-unit

.PHONY: test test-unit test-race test-cover bench fuzz fuzz-long lint lint-fix

#################
###  Install  ###
#################

all: install

install:
	@echo "--> ensure dependencies have not been modified"
	@go mod verify
	@echo "--> installing $(APPNAME)d"
	@go install $(BUILD_FLAGS) -mod=readonly ./cmd/$(APPNAME)d

.PHONY: all install

##################
###  Protobuf  ###
##################

# Use this target if you do not want to use Ignite for generating proto files

proto-deps:
	@echo "Installing proto deps (buf)..."
	@go install github.com/bufbuild/buf/cmd/buf@latest
	@echo "Buf CLI installed. Ensure $$(go env GOPATH)/bin is in your PATH."

proto-gen:
	@echo "Generating protobuf files..."
	@ignite generate proto-go --yes
	@echo "Generating shared chain protos with Buf..."
	@cd .. && buf generate proto

.PHONY: proto-gen

#################
###  Linting  ###
#################

# Pre-download golangci-lint into module cache (for CI shared cache or local use).
lint-deps:
	@echo "--> Pre-downloading golangci-lint (module cache)"
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint

lint:
	@echo "--> Running linter"
	@go tool github.com/golangci/golangci-lint/cmd/golangci-lint run ./... --timeout 15m

lint-fix:
	@echo "--> Running linter with auto-fix"
	@go fmt ./...
	@go tool github.com/golangci/golangci-lint/cmd/golangci-lint run ./... --fix --timeout 15m

###################
### Development ###
###################

govet:
	@echo Running go vet...
	@go vet ./...

govulncheck:
	@echo Running govulncheck...
	@go tool golang.org/x/vuln/cmd/govulncheck ./...

.PHONY: govet govulncheck lint-deps
