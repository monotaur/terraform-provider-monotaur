BINARY_NAME      := terraform-provider-monotaur
PROVIDER_ADDRESS := registry.terraform.io/monotaur/monotaur

# Default Go build flags.
BUILD_FLAGS := -ldflags="-X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)"

.PHONY: build
build: ## Compile the provider binary
	go build $(BUILD_FLAGS) -o $(BINARY_NAME) .

.PHONY: install
install: build ## Install the provider into the local Terraform plugin cache
	@GOARCH=$$(go env GOARCH); \
	GOOS=$$(go env GOOS); \
	PLUGIN_DIR=$$HOME/.terraform.d/plugins/$(PROVIDER_ADDRESS)/0.1.0/$${GOOS}_$${GOARCH}; \
	mkdir -p $$PLUGIN_DIR; \
	cp $(BINARY_NAME) $$PLUGIN_DIR/$(BINARY_NAME); \
	echo "Installed to $$PLUGIN_DIR"

.PHONY: test
test: ## Run unit tests
	go test ./...

.PHONY: testacc
testacc: ## Run acceptance tests against a live Monotaur API (requires TF_ACC=1, MONOTAUR_ENDPOINT, MONOTAUR_API_KEY)
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: generate
generate: ## Re-generate any generated code
	go generate ./...

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)

.PHONY: help
help: ## Print this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
