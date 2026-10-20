# Project instructions

Republic is a Go provider library. Keep gateways, agent loops, and tool execution outside the package. Use English in repository files and GitHub contributions.

Run `make check`, `go test -race ./...`, `make parity`, and `go build ./...` before publishing implementation changes. Compatibility baselines in `testdata/compatibility` preserve original behavior and run with Go alone; do not regenerate expected outputs from the Go implementation. Main checks Go on Linux, macOS and Windows and the minimum supported Go version. Documentation uses the external Zensical tool through `make docs-test`; it has no project package or dependency environment.

For fixes, use a task-named branch and the prepared Git identity. Publish the candidate PR, then start native CI explicitly when using the workflow token:

```bash
gh workflow run main.yml --ref BRANCH -f number=PR_NUMBER -f head=CANDIDATE_SHA
```

Use the full candidate SHA for dispatch. Link the candidate and pending checks in the requested reply; do not wait for the Landing feedback job or its enclosing workflow. A push made with the workflow token does not start CI automatically.
