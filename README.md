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

The E2E suite needs a running Monotaur API at `MONOTAUR_ENDPOINT` plus a
write-scoped `MONOTAUR_API_KEY`. If you have an existing instance you can
reach, just export those two variables and run `make e2e`. Otherwise the
recommended local setup is a [KIND](https://kind.sigs.k8s.io/) cluster with the
[monotaur-chart](https://github.com/monotaur/monotaur-chart) installed —
exactly what nightly CI does (see `.github/workflows/e2e.yml`).

### Prerequisites

- [`kind`](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [`helm`](https://helm.sh/docs/intro/install/) v3.17+
- [`kubectl`](https://kubernetes.io/docs/tasks/tools/)
- A GitHub personal access token with `read:packages` scope (the Monotaur
  container image lives in private GHCR). Set:
  ```bash
  export GHCR_USERNAME=<your-github-username>
  export GHCR_TOKEN=<your-pat>
  ```

### 1. Create a cluster

```bash
kind create cluster --name monotaur-e2e
kubectl create namespace monotaur
```

### 2. Install Postgres and Redis

The Monotaur chart consumes pre-existing connection-string Secrets, so the DB
and cache go in first:

```bash
helm install pg oci://registry-1.docker.io/bitnamicharts/postgresql \
  -n monotaur \
  --set auth.username=monotaur \
  --set auth.database=monotaur \
  --set auth.password=testpassword123 \
  --set primary.persistence.enabled=false \
  --set readReplicas.persistence.enabled=false \
  --wait --timeout 5m

helm install rd oci://registry-1.docker.io/bitnamicharts/redis \
  -n monotaur \
  --set auth.enabled=false \
  --set master.persistence.enabled=false \
  --set replica.replicaCount=0 \
  --wait --timeout 5m
```

### 3. Create the prerequisite Secrets

```bash
kubectl -n monotaur create secret generic monotaur-postgres \
  --from-literal=connection-string='Host=pg-postgresql;Port=5432;Database=monotaur;Username=monotaur;Password=testpassword123'

kubectl -n monotaur create secret generic monotaur-redis \
  --from-literal=connection-string='rd-redis-master:6379'

kubectl -n monotaur create secret generic monotaur-api-keys \
  --from-literal=monotaur-worker=ci-dummy-worker-key

kubectl -n monotaur create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io \
  --docker-username="$GHCR_USERNAME" \
  --docker-password="$GHCR_TOKEN"
```

### 4. Install the Monotaur chart

The chart itself is also private, so authenticate Helm to GHCR before pulling:

```bash
echo "$GHCR_TOKEN" | helm registry login ghcr.io \
  --username "$GHCR_USERNAME" --password-stdin

helm install monotaur oci://ghcr.io/monotaur/charts/monotaur-chart \
  -n monotaur \
  --values scripts/ci-values.yaml \
  --wait --timeout 10m
```

`scripts/ci-values.yaml` pins the image tag, wires the `ghcr-pull` imagePullSecret,
and disables the worker subchart. Same overlay CI uses.

### 5. Extract the bootstrap API key

On first start Monotaur writes an admin key into a Secret called
`monotaur-bootstrap-credentials`. Wait for it, then decode:

```bash
kubectl -n monotaur wait --for=create secret/monotaur-bootstrap-credentials --timeout=5m

export MONOTAUR_API_KEY=$(kubectl -n monotaur get secret monotaur-bootstrap-credentials \
  -o jsonpath='{.data.bootstrap\.key}' | base64 -d)
```

### 6. Port-forward and run the suite

```bash
kubectl -n monotaur port-forward svc/monotaur-monotaur-core 5000:5000 &
export MONOTAUR_ENDPOINT=http://127.0.0.1:5000

make e2e
```

### Cleanup

```bash
kill %1                                # stop the port-forward
kind delete cluster --name monotaur-e2e
```

See [docs/e2e.md](docs/e2e.md) for the full contributor guide — naming
convention, sweeper behavior, reading output files, and troubleshooting.

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
