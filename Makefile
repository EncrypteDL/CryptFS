-include environ.inc
.PHONY: help deps dev build install image release test clean

export CGO_ENABLED=0
VERSION=$(shell git describe --abbrev=0 --tags 2>/dev/null || echo "0.0.0")
COMMIT=$(shell git rev-parse --short HEAD || echo "HEAD")
BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
GOCMD=go
GOVER=$(shell go version | grep -o -E 'go1\.17\.[0-9]+')

DESTDIR=/usr/local/bin

ifeq ($(LOCAL), 1)
IMAGE := r.mills.io/prologic/CryptFS
TAG := dev
else
ifeq ($(BRANCH), main)
IMAGE := prologic/CryptFS
TAG := latest
else
IMAGE := prologic/CryptFS
TAG := dev
endif
endif

all: help

.PHONY: help

help: ## Show this help message
	@echo "CryptFS - a distributed file system"
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m\033[0m\n"} /^[$$()% a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

preflight: ## Run preflight checks to ensure you have the right build tools
	@./preflight.sh

deps: ## Install any dependencies required

dev : DEBUG=1
dev : build ## Build debug version of CryptFS
	@./CryptFS -v
	@./setup-local-cluster.sh

cli: ## Build the CryptFS command-line
	@$(GOCMD) build -tags "netgo static_build" -installsuffix netgo \
		-ldflags "-w \
		-X git.mills.io/prologic/CryptFS/internal.Version=$(VERSION) \
		-X git.mills.io/prologic/CryptFS/internal.Commit=$(COMMIT)" \
		./cmd/CryptFS/...

build: cli ## Build the cli and the server

generate: ## Genereate any code required by the build
	@if [ x"$(DEBUG)" = x"1"  ]; then		\
	  echo 'Running in debug mode...';	\
	fi

install: build ## Install CryptFS to $DESTDIR
	@install -D -m 755 CryptFS $(DESTDIR)/CryptFS

ifeq ($(PUBLISH), 1)
image: generate ## Build the Docker image
	@docker buildx build \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg BUILD="$(BUILD)" \
		--platform linux/amd64,linux/arm64 --push -t $(IMAGE):$(TAG) .
else
image: generate
	@docker build  \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg BUILD="$(BUILD)" \
		-t $(IMAGE):$(TAG) .
endif

release: generate ## Release a new version to Gitea
	@./tools/release.sh

fmt: ## Format sources fiels
	@$(GOCMD) fmt ./...

test: ## Run test suite
	@CGO_ENABLED=1 $(GOCMD) test -v -cover -race -cover -coverprofile=coverage.out  ./...

clean: ## Remove untracked files
	@git clean -f -d
