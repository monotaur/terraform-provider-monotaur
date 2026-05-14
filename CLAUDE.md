# CLAUDE.md — Monotaur Terraform Provider

This file is the authoritative guide for Claude (and any automated agent) working in this repository.

## Repository Overview

This repository contains the Terraform provider for [Monotaur](https://monotaur.io), built with the [Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework). The provider is written in Go and tested against a live Monotaur API instance using acceptance tests.

## Common Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Compile the provider binary |
| `make test` | Run unit tests |
| `make lint` | Run golangci-lint |
| `make e2e` | Run the full E2E suite (see below) |
| `make e2e-one TEST=<name>` | Run a single E2E test |
| `make e2e-sweep` | Delete all `tfe2e-*` resources from staging |

## Running the E2E Suite

Required environment variables:

- `MONOTAUR_ENDPOINT` — staging API endpoint (e.g. `https://api.staging.monotaur.io`)
- `MONOTAUR_API_KEY` — staging API key with write access

Optional environment variables:

- `MONOTAUR_LOG_LEVEL` — log verbosity for the provider during tests (e.g. `DEBUG`)

Run the full suite:

```bash
export MONOTAUR_ENDPOINT=https://api.staging.monotaur.io
export MONOTAUR_API_KEY=<your-key>
make e2e
```

Run a single test:

```bash
make e2e-one TEST=TestAccMonotaurLabel_basic
```

After running, read `e2e-results/summary.md`. A `Result: PASS` line and exit code 0 means the suite passed. Any other result means failure — check `e2e-results/junit.xml` and `e2e-results/raw.jsonl` for details.

**Claude: read `e2e-results/summary.md` after `make e2e`; exit 0 + `Result: PASS` line means success.**

### Output files

| File | Contents |
|------|----------|
| `e2e-results/summary.md` | Human- and machine-readable summary: `Result: PASS` or `Result: FAIL`, test counts, names of failing tests |
| `e2e-results/junit.xml` | JUnit XML report for CI artifact upload |
| `e2e-results/raw.jsonl` | Raw `go test -json` event stream for verbose debugging |

All three files are git-ignored and written fresh on every run.

### On failure

1. Read `e2e-results/summary.md` to identify failing test names.
2. Reproduce locally: `make e2e-one TEST=<failing-test-name>`
3. Inspect `e2e-results/raw.jsonl` for API-level error messages.
4. If staging state is dirty, run `make e2e-sweep` to delete leftover `tfe2e-*` resources before re-running.

See [docs/e2e.md](docs/e2e.md) for the full contributor guide, including common error messages and debugging steps.

## Code Structure

```
internal/provider/   — provider resources and data sources
internal/acctest/    — shared acceptance-test helpers (resource naming, etc.)
scripts/             — shell helpers (e2e-preflight.sh)
docs/                — contributor documentation
e2e-results/         — test output (git-ignored, created at runtime)
```

## Resource Naming Convention

E2E tests name every resource they create as:

```
tfe2e-<6-char-hex-prefix>-<resourceType>-<n>
```

Example: `tfe2e-a3f9c2-label-1`

The prefix is stable for the lifetime of one test process, so all resources from a single run share it. This makes cleanup unambiguous and `make e2e-sweep` safe to run at any time.
