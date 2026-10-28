# End-to-End Tests

End-to-end (E2E) tests verify the provider against a live Monotaur API instance.
They are acceptance tests gated on `TF_ACC=1` and are run via `gotestsum` for
structured output and JUnit XML reporting.

## Prerequisites

### Go version

Go 1.21 or later is required. Check with:

```bash
go version
```

### Build tools

- `make` — all E2E targets are exposed as Makefile rules
- `gotestsum` — fetched automatically via `go run gotest.tools/gotestsum`; no manual install needed

### Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MONOTAUR_ENDPOINT` | yes | Base URL of the Monotaur API, e.g. `https://api.staging.monotaur.io` |
| `MONOTAUR_API_KEY` | yes | API key with write access (create, update, delete) to the staging environment |
| `MONOTAUR_LOG_LEVEL` | no | Provider log verbosity during tests, e.g. `DEBUG` |

Both required variables must be set before running any E2E target. The
preflight script (`scripts/e2e-preflight.sh`) validates them and aborts with a
one-line error message if either is missing.

### Running API instance

Tests need a running Monotaur API at `MONOTAUR_ENDPOINT` with a key that can
create and delete every resource type the suite exercises. Any of the following
works:

- A shared staging environment, if your team runs one — contact them for the
  endpoint and a write-scoped API key.
- A local install via the [monotaur-chart](https://github.com/monotaur/monotaur-chart)
  on KIND/minikube, port-forwarded to `localhost`. This is what CI does — see
  `.github/workflows/e2e.yml` for the exact sequence.
- Any other instance you can reach over the network, provided the API key has
  full write access.

## Running the suite

### Full suite

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

This runs the preflight check, then executes all tests under `./internal/provider/...`
with a 30-minute timeout. Output is written to `e2e-results/`.

`make e2e` automatically runs the resource sweeper (`make e2e-sweep`) before
the test suite to remove any resources that were leaked by previous runs.

### Single test

```bash
make e2e-one TEST=TestAccMonotaurLabel_basic
```

Use this for targeted iteration. The timeout is 10 minutes for single-test runs.

### Intentional re-runs

Use `make e2e-one` for any retry — it is always safe to re-run because the
sweeper runs automatically as part of `make e2e` before the suite starts. For
`make e2e-one`, you may want to run `make e2e-sweep` first if you suspect stale
state from a previous interrupted run.

## Reading the output

### `e2e-results/summary.md`

This is the primary output file. After `make e2e` completes, read it to
determine whether the suite passed or failed.

**Claude: read `e2e-results/summary.md` after `make e2e`; exit 0 + `Result: PASS` line means success.**

The file contains:

- `Result: PASS` — all tests passed, or `Result: FAIL` — one or more tests failed
- Total test count, pass count, and fail count
- Names of any failing tests

Example passing summary:

```
Result: PASS
Tests: 12 passed, 0 failed, 0 skipped
```

Example failing summary:

```
Result: FAIL
Tests: 11 passed, 1 failed, 0 skipped

Failed tests:
- TestAccMonotaurLabel_update
```

The summary is written by the `e2e-reporter` tool (see issue #63). Until that
tool ships, `summary.md` will not be present; rely on the `make e2e` exit code
and `junit.xml` instead.

### `e2e-results/junit.xml`

JUnit XML report suitable for upload to CI artifact stores (GitHub Actions,
GitLab CI, etc.). Every test case is listed with its pass/fail status and
duration.

### `e2e-results/raw.jsonl`

Raw `go test -json` event stream — one JSON object per line. Use this for
verbose debugging when you need full log output, API responses, or Terraform
plan diffs.

```bash
# Show only FAIL events
grep '"Action":"fail"' e2e-results/raw.jsonl | jq .

# Show all output for a specific test
grep 'TestAccMonotaurLabel_update' e2e-results/raw.jsonl | jq -r '.Output // empty'
```

### `e2e-results/sweep.txt`

Sweep summary (deletions and any errors) written by `make e2e-sweep`.

All output files are git-ignored and re-created on every run.

## Resource naming

Tests use `internal/acctest.Name(resourceType, n)` to generate collision-free
resource names scoped to the current process run:

```
tfe2e-<6-char-hex-prefix>-<resourceType>-<n>
```

Example: `tfe2e-a3f9c2-label-1`

The prefix is generated once per process (via `acctest.RunPrefix()`) so all
resources created in a single run share the same prefix. This serves two
purposes:

1. **Safe cleanup** — the sweeper can delete exactly the resources from a
   specific run without touching unrelated staging data.
2. **Collision avoidance** — concurrent CI runs use distinct prefixes and
   cannot interfere with each other's resources.

When writing a new test, always use `acctest.Name(...)` for resource names.
Never use hard-coded strings.

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
- `make e2e-sweep` exits non-zero if any sweeper returns an error, making it safe
  to use as a gate in CI pipelines.

Key facts:

- The sweeper runs automatically before `make e2e` to clear any leftover state
  from previous interrupted runs.
- Nightly CI also runs `make e2e-sweep` as a standalone job to keep staging tidy.
- Run `make e2e-sweep` manually any time you want to clean up staging without
  running the full suite.

## Nightly CI

`.github/workflows/e2e.yml` runs the full E2E suite nightly at 02:00 UTC
against an ephemeral [KIND](https://kind.sigs.k8s.io/) cluster spun up inside
the GitHub Actions runner. Each run:

1. Starts a fresh KIND cluster.
2. Installs Bitnami `postgresql` and `redis` charts (persistence disabled).
3. Creates the prerequisite Secrets the Monotaur chart consumes
   (`monotaur-postgres`, `monotaur-redis`, `monotaur-api-keys`) plus a
   `ghcr-pull` `imagePullSecrets` built from `${{ secrets.GITHUB_TOKEN }}`.
4. Installs the Monotaur chart from `oci://ghcr.io/monotaur/charts/monotaur-chart`
   with `scripts/ci-values.yaml`.
5. Waits for the `monotaur-bootstrap-credentials` Secret to appear and decodes
   its admin API key (`scripts/ci-extract-bootstrap-key.sh`).
6. Background-runs `kubectl port-forward` to expose the Monotaur API on
   `http://127.0.0.1:5000`.
7. Runs `make e2e` against that local endpoint.

The same workflow also runs on `workflow_dispatch` and on pull requests labeled
`run-e2e`. The cluster is torn down with the runner — no shared state between
runs, so the `make e2e` sweeper step is a no-op in CI but stays in place for
local-dev use.

On failure, the workflow uploads `e2e-results/cluster-diag/**` (pod describes,
container logs, `helm status`, event stream) and `port-forward.log` alongside
the usual `junit.xml` / `summary.md` / `raw.jsonl` artifacts.

### Parked staging sweeper

`.github/workflows/e2e-sweep.yml` previously swept a shared staging
environment nightly. Its cron trigger is commented out — there is no shared
environment to sweep under the KIND-only approach. The file is retained with
`workflow_dispatch` only so the job can be revived if a staging environment is
reintroduced; uncomment the `schedule:` block and re-configure
`MONOTAUR_STAGING_*` secrets to bring it back.

## Debugging a failing test

1. **Identify the failing test name** from `e2e-results/summary.md` (or
   `junit.xml`).

2. **Reproduce in isolation:**
   ```bash
   make e2e-one TEST=TestAccMonotaurLabel_update
   ```

3. **Inspect raw output** for API error messages:
   ```bash
   grep 'TestAccMonotaurLabel_update' e2e-results/raw.jsonl | jq -r '.Output // empty'
   ```

4. **Check staging state.** If a previous run left resources behind, the test
   may fail because a resource already exists. Run the sweeper first:
   ```bash
   make e2e-sweep
   make e2e-one TEST=TestAccMonotaurLabel_update
   ```

5. **Enable debug logging** for more verbose provider output:
   ```bash
   export MONOTAUR_LOG_LEVEL=DEBUG
   make e2e-one TEST=TestAccMonotaurLabel_update
   ```

## Common errors

### `Error: MONOTAUR_ENDPOINT not set — see docs/e2e.md`

The preflight script rejected the run. Export the required variables before
invoking any E2E target:

```bash
export MONOTAUR_ENDPOINT=https://api.staging.monotaur.io
export MONOTAUR_API_KEY=<your-key>
```

### `Error: MONOTAUR_API_KEY not set — see docs/e2e.md`

Same as above — the `MONOTAUR_API_KEY` variable is missing.

### `401 Unauthorized`

The API key is invalid, expired, or does not have write access to the staging
environment. Verify the key is correct and has not been rotated. Obtain a new
key from your team if needed.

### Rate limit errors

If the suite triggers API rate limits, the affected tests will fail with HTTP
429 responses. Options:

- Reduce parallelism: the suite currently runs tests sequentially within a
  package; no additional flags are needed for most cases.
- If running multiple suite invocations back-to-back, add a short pause between
  runs.
- Run only the failing test with `make e2e-one TEST=<name>` rather than the
  full suite.

### Leftover state / pre-existing resource name conflicts

If `make e2e` was interrupted (Ctrl-C, OOM, timeout), resources from the
aborted run may still exist in staging. The next run will generate a new
process prefix, so name collisions are rare — but if you see errors like
`resource already exists` or unexpected plan diffs, run the sweeper first:

```bash
make e2e-sweep
make e2e
```

### Tests are skipped unexpectedly

`TF_ACC=1` is injected automatically by all E2E Makefile targets. If you are
running `go test` directly, set it manually:

```bash
TF_ACC=1 go test -run TestAccMonotaurLabel_basic -v ./internal/provider/...
```

### Timeout exceeded

The full suite uses a 30-minute timeout. Individual test runs via `make e2e-one`
use 10 minutes. If a test legitimately needs more time (e.g. a slow API
operation), adjust the timeout with `TESTARGS`:

```bash
TESTARGS="-timeout 20m" make e2e-one TEST=TestAccMonotaurLabel_basic
```
