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

.PHONY: e2e
e2e: ## Run the full E2E suite (requires MONOTAUR_ENDPOINT, MONOTAUR_API_KEY — see docs/e2e.md)
	@bash scripts/e2e-preflight.sh
	@mkdir -p e2e-results
	TF_ACC=1 go run gotest.tools/gotestsum \
		--format pkgname-and-test-fails \
		--junitfile e2e-results/junit.xml \
		--jsonfile e2e-results/raw.jsonl \
		-- -v -count=1 -timeout 30m ./internal/provider/...

.PHONY: e2e-one
e2e-one: ## Run a single E2E test (requires TEST=<name>, MONOTAUR_ENDPOINT, MONOTAUR_API_KEY — see docs/e2e.md)
	@bash scripts/e2e-preflight.sh
	@if [ -z "$(TEST)" ]; then echo "Error: TEST is required — usage: make e2e-one TEST=TestAccMonotaurMonitor_basic"; exit 1; fi
	@mkdir -p e2e-results
	TF_ACC=1 go run gotest.tools/gotestsum \
		--format pkgname-and-test-fails \
		-- -v -run $(TEST) -count=1 -timeout 10m ./internal/provider/...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: generate
generate: ## Re-generate any generated code
	go generate ./...

.PHONY: docs
docs: ## Generate Registry-compatible Markdown docs via tfplugindocs
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name monotaur

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)

.PHONY: help
help: ## Print this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
