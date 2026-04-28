# End-to-End Tests

End-to-end (E2E) tests verify the provider against a live Monotaur API instance.
They are acceptance tests gated on `TF_ACC=1` and are run via `gotestsum` for
structured output and JUnit XML reporting.

## Prerequisites

| Environment variable  | Description |
|-----------------------|-------------|
| `MONOTAUR_ENDPOINT`   | Base URL of the Monotaur API, e.g. `https://api.staging.monotaur.io` |
| `MONOTAUR_API_KEY`    | API key with sufficient permissions to create and delete resources |

Both variables must be set before running any E2E target. A preflight check
will abort with a one-line error message and a non-zero exit code if either is
missing.

## Running the suite

```bash
export MONOTAUR_ENDPOINT=https://api.staging.monotaur.io
export MONOTAUR_API_KEY=<your-key>

# Run the full suite
make e2e

# Run a single test
make e2e-one TEST=TestAccMonotaurMonitor_basic
```

## Output files

After a run, two files are written to `e2e-results/` (git-ignored):

| File | Contents |
|------|----------|
| `e2e-results/junit.xml` | JUnit XML report, suitable for CI artifact upload |
| `e2e-results/raw.jsonl` | Raw `go test -json` event stream (one JSON object per line) |

The directory is created automatically if it does not exist.

## Exit codes

`make e2e` and `make e2e-one` forward the exit code from `go test` directly:

- `0` — all tests passed
- non-zero — one or more tests failed or the suite was interrupted

## Resource naming

Tests use `internal/acctest.Name(resourceType, n)` to generate collision-free
resource names scoped to the current process run:

```
tfe2e-<6-char-hex-prefix>-<resourceType>-<n>
```

Example: `tfe2e-a3f9c2-monitor-1`

The prefix is generated once per process (via `acctest.RunPrefix()`) so all
resources created in a single run share the same prefix, making cleanup easy to
identify.

## Troubleshooting

**`Error: MONOTAUR_ENDPOINT not set — see docs/e2e.md`**
Export the variable before running `make e2e`.

**Tests are skipped**
`TF_ACC=1` is injected automatically by the Makefile target. If running
`go test` directly, set `TF_ACC=1` manually.

**Timeouts**
The full suite uses a 30-minute timeout (`-timeout 30m`). Individual test runs
via `make e2e-one` use 10 minutes. Adjust with `TESTARGS` on the `testacc`
target if needed.

**Stale resources**
If a test run is interrupted, resources with the `tfe2e-<prefix>-` prefix may
be left in the staging environment. Search for that prefix to identify and
delete them.
