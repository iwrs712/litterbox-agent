GO ?= go
GOPATH_BIN ?= $(shell $(GO) env GOPATH)/bin
BUF ?= $(GOPATH_BIN)/buf
PROTOC_GEN_GO ?= $(GOPATH_BIN)/protoc-gen-go
PROTOC_GEN_CONNECT_GO ?= $(GOPATH_BIN)/protoc-gen-connect-go

VERSION ?= dev
LDFLAGS ?= -s -w
NODE_INSTALLER_IMAGE ?= envd-node-installer
ENVD_VERSION ?= 0.1.5

.PHONY: all tools generate fmt test vet build build-linux-amd64 clean build-node-installer push-node-installer

all: test build

tools:
	GOBIN=$(GOPATH_BIN) $(GO) install github.com/bufbuild/buf/cmd/buf@v1.50.0
	GOBIN=$(GOPATH_BIN) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6
	GOBIN=$(GOPATH_BIN) $(GO) install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.18.1

generate: tools
	PATH=$(GOPATH_BIN):$$PATH $(BUF) generate

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='$(LDFLAGS)' -o bin/envd ./cmd/envd

build-linux-amd64:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags='$(LDFLAGS)' -o bin/envd-linux-amd64 ./cmd/envd

clean:
	rm -rf bin

build-node-installer:
	docker buildx build --platform linux/amd64 \
	  -f deploy/node-installer/Dockerfile \
	  -t $(NODE_INSTALLER_IMAGE):$(ENVD_VERSION)-amd64 --load .

push-node-installer:
	docker push $(NODE_INSTALLER_IMAGE):$(ENVD_VERSION)-amd64
