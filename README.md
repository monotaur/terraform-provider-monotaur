# Terraform Provider for Monotaur

This repository contains the Terraform provider for [Monotaur](https://monotaur.io),
built with the [Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework).

## Usage

```hcl
terraform {
  required_providers {
    monotaur = {
      source  = "monotaur/monotaur"
      version = "~> 0.1"
    }
  }
}
```

## Requirements

- [Go](https://golang.org/doc/install) 1.21+
- [Terraform](https://developer.hashicorp.com/terraform/downloads) 1.5+

## Building the Provider

```sh
make build
```

This produces a `terraform-provider-monotaur` binary in the repo root.

## Installing Locally (dev override)

The recommended workflow for local development is a
[dev override](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers)
that tells Terraform to load the provider from your local filesystem instead of
the public registry.

### 1. Build the binary

```sh
make build
```

### 2. Configure a dev override

Create (or append to) `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/monotaur/monotaur" = "/path/to/this/repo"
  }

  # Fall through to the public registry for all other providers.
  direct {}
}
```

Replace `/path/to/this/repo` with the absolute path to the directory that
contains the compiled binary.

### 3. Write a test configuration

```hcl
terraform {
  required_providers {
    monotaur = {
      source = "registry.terraform.io/monotaur/monotaur"
    }
  }
}

provider "monotaur" {
  # endpoint and api_key can also be supplied via env vars:
  #   MONOTAUR_ENDPOINT
  #   MONOTAUR_API_KEY
}
```

### 4. Verify the provider is recognized

```sh
terraform providers
```

With the dev override active you will see a warning that dev overrides are in
use, and the `monotaur` provider will be listed with no version constraint.

## Provider Configuration

| Attribute | Type   | Required | Description |
|-----------|--------|----------|-------------|
| `endpoint` | string | no | Base URL of the Monotaur API. Falls back to `MONOTAUR_ENDPOINT` env var. |
| `api_key`  | string | no | API key for authentication. Falls back to `MONOTAUR_API_KEY` env var. Marked sensitive. |

## Quick Start (E2E Testing)

To run the provider's end-to-end test suite against the staging API, set two
environment variables and run `make e2e`:

```bash
export MONOTAUR_ENDPOINT=https://api.staging.monotaur.io
export MONOTAUR_API_KEY=<your-staging-key>
make e2e
```

See [docs/e2e.md](docs/e2e.md) for the full contributor guide — prerequisites,
per-test naming convention, sweeper behavior, reading output files, and
troubleshooting common errors.

## Development

| Command        | Description                                |
|----------------|--------------------------------------------|
| `make build`   | Compile the provider binary                |
| `make install` | Build and copy binary to local plugin cache |
| `make test`    | Run all unit tests                         |
| `make lint`    | Run golangci-lint                          |
| `make e2e`     | Run the full E2E suite (see docs/e2e.md)   |
| `make clean`   | Remove build artifacts                     |

## License

Mozilla Public License 2.0 — see [LICENSE](LICENSE).
