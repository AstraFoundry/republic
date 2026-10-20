# Contributing

Republic is a Go provider library. Keep gateways, agent loops and tool execution
in caller code. Repository files and GitHub contributions use English.

## Setup

Install Go 1.23+ and run `make install`. Create a task-named branch such
as `codex/fix-stream-completion`.

The Go package is at the repository root. Frozen original-behavior baselines are
in `testdata/compatibility`; raw protocol recordings, licenses and attribution are
in `testdata/protocol`. Tests use these data files directly. Do not regenerate
expected outputs from the implementation under test. Change an expectation only
when the intended public behavior changes and explain the evidence in the PR.

## Validation

Before publishing implementation changes:

```sh
make check
make test
make parity
make build
make docs-test
```

`make check` runs Go formatting, vet and patch whitespace checks.
`make test` includes the race detector and all compatibility checks. `make parity`
runs the 570 request and 58 response baselines plus protocol invariants explicitly;
all these commands require only Go. Tests use local HTTP servers, temporary
credential stores and recorded responses. No live provider credentials are needed.

Documentation is optional for Go development. It uses the third-party Zensical
0.0.69 tool through `uvx`, installed in an isolated tool environment. Install uv
and run `make docs-test` to validate documentation or `make docs` to preview it.
There is no project Python package, virtual environment setup or dependency lock.
General repository hooks can be run with `prek run --all-files` after installing
prek separately; there are no Python-specific lint or type-check jobs.

Run `make fuzz` when changing message serialization or protocol parsers. Add
regression cases for failures, not just happy paths. Check stream cancellation,
metadata replay, history writes, retries and client ownership when affected.

CI tests Go on Linux, macOS and Windows, checks the minimum Go version, and
validates the baselines without a Python subprocess. Go modules are consumed
from semantic Git tags.

## Publishing a candidate

Push the task branch and open a candidate PR. When using the workflow token,
explicitly dispatch native CI with the full candidate commit SHA:

```sh
gh workflow run main.yml --ref BRANCH -f number=PR_NUMBER -f head=CANDIDATE_SHA
```

Link the PR and pending checks. Do not wait for the Landing feedback job.
Go releases use semantic Git tags; publishing a candidate does not create a
release or deploy documentation.
