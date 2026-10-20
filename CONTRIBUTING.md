# Contributing

Republic is a Go provider library. Keep gateways, agent loops and tool execution
in caller code. Repository files and GitHub contributions use English.

## Setup

Install Go 1.23+ and uv, then run `make install`. Create a task-named branch such
as `codex/fix-stream-completion`.

The Go package is at the repository root. The retained Python package under
`src/republic` is an executable compatibility oracle. Do not modify its behavior
to make a Go parity test pass. Original protocol recordings, licenses and
attribution remain in `tests/test_provider_golden`.

## Validation

Before publishing implementation changes:

```sh
make check
make test
make parity
make build
make docs-test
```

`make check` runs Go formatting/vet plus the required
`uv run prek run --all-files` and `uv run ty check`.
`make test` includes the race detector. `make parity` runs live differential
request/response comparisons and all original Python behavior tests. Tests use
local HTTP servers, temporary credential stores and recorded responses.

Run `make fuzz` when changing message serialization or protocol parsers. Add
regression cases for failures, not just happy paths. Check stream cancellation,
metadata replay, history writes, retries and client ownership when affected.

CI tests Go on Linux, macOS and Windows, checks the minimum Go version, and runs
the Python oracle across Python 3.11–3.14. Python package builds continue as
an oracle integrity check; the Go module is consumed from Git tags.

## Publishing a candidate

Push the task branch and open a candidate PR. When using the workflow token,
explicitly dispatch native CI with the full candidate commit SHA:

```sh
gh workflow run main.yml --ref BRANCH -f number=PR_NUMBER -f head=CANDIDATE_SHA
```

Link the PR and pending checks. Do not wait for the Landing feedback job.
Go releases use semantic Git tags; publishing a candidate does not create a
release or deploy documentation.
