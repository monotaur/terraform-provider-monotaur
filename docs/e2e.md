# End-to-End Testing

This document describes how to run the end-to-end acceptance test suite and how
the resource sweeper cleans up leaked test infrastructure.

## Prerequisites

The acceptance tests exercise a live Monotaur API. You need:

| Variable              | Description                                      |
|-----------------------|--------------------------------------------------|
| `MONOTAUR_ENDPOINT`   | Base URL of the staging Monotaur API             |
| `MONOTAUR_API_KEY`    | API key with write access to staging             |
| `TF_ACC`              | Must be set to `1` to enable acceptance tests    |

## Running acceptance tests

```shell
export MONOTAUR_ENDPOINT=https://staging.example.com
export MONOTAUR_API_KEY=your-api-key
export TF_ACC=1

make e2e
```

`make e2e` automatically runs the resource sweeper before the test suite to
remove any resources that were leaked by previous runs.

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

### Running the sweeper standalone

```shell
export MONOTAUR_ENDPOINT=https://staging.example.com
export MONOTAUR_API_KEY=your-api-key

make e2e-sweep
```

The `e2e-sweep` target exits non-zero if any sweeper returns an error, making
it safe to use as a gate in CI pipelines.

## Nightly safety net

A GitHub Actions workflow (`.github/workflows/e2e-sweep.yml`) runs the sweeper
against staging every night at 03:00 UTC and on manual dispatch. It uses the
`MONOTAUR_STAGING_ENDPOINT` and `MONOTAUR_STAGING_API_KEY` repository secrets.

This provides a safety net for resources leaked by any workflow — including
runs that were cancelled or timed out — and keeps the staging environment clean
between scheduled test runs.
