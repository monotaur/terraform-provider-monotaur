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

# Run the full suite (sweeper runs first automatically)
make e2e

# Run a single test
make e2e-one TEST=TestAccMonotaurMonitor_basic

# Run the sweeper standalone
make e2e-sweep
```

`make e2e` automatically runs the resource sweeper (`make e2e-sweep`) before
the test suite to remove any resources that were leaked by previous runs.

## Output files

After a run, two files are written to `e2e-results/` (git-ignored):

| File | Contents |
|------|----------|
| `e2e-results/junit.xml` | JUnit XML report, suitable for CI artifact upload |
| `e2e-results/raw.jsonl` | Raw `go test -json` event stream (one JSON object per line) |
| `e2e-results/sweep.txt` | Sweep summary (deletions and any errors) |

The directory is created automatically if it does not exist.

## Exit codes

`make e2e` and `make e2e-one` forward the exit code from `go test` directly:

- `0` — all tests passed
- non-zero — one or more tests failed or the suite was interrupted

`make e2e-sweep` exits non-zero if any sweeper returns an error, making it safe
to use as a gate in CI pipelines.

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

## Sweepers

### Purpose

Each acceptance test creates real resources on the staging API. When a test
fails mid-flight — or when the test process is killed — those resources are
left behind. Sweepers clean them up by deleting any resource whose name starts
with the `tfe2e-` prefix.

### Naming convention

All test resources must be named with the `tfe2e-` prefix (e.g. `tfe2e-my-label`).
Sweepers use `strings.HasPrefix(name, "tfe2e-")` to identify and delete test
resources without touching production data.

Resources that have no name of their own (alarms, probes, monitor status rules,
role assignments) are swept by cross-referencing their parent resource. For
example, a probe is deleted when its parent monitor has a `tfe2e-` name.

### Deletion order

Sweepers delete resources in dependency order so that child resources are
removed before their parents:

1. `api_key`
2. `role_assignment`
3. `role`
4. `service_account`
5. `monitor_status_rule`
6. `alarm`
7. `variable`
8. `secret`
9. `probe`
10. `sensor`
11. `monitor`
12. `component`
13. `label`

### Sweeper behaviour

- Each deletion is logged at the `[INFO]` level with the resource type, name, and ID.
- A failure to delete a single resource is non-fatal: the sweeper logs a
  `[WARN]` and continues to the next resource.
- The total number of deletions is emitted at the end.
- A summary is written to `e2e-results/sweep.txt` (the directory is gitignored).

## Nightly safety net

A GitHub Actions workflow (`.github/workflows/e2e-sweep.yml`) runs the sweeper
against staging every night at 03:00 UTC and on manual dispatch. It uses the
`MONOTAUR_STAGING_ENDPOINT` and `MONOTAUR_STAGING_API_KEY` repository secrets.

This provides a safety net for resources leaked by any workflow — including
runs that were cancelled or timed out — and keeps the staging environment clean
between scheduled test runs.

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
be left in the staging environment. Run `make e2e-sweep` or search for that
prefix to identify and delete them manually.
