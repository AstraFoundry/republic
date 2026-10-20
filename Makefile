.PHONY: install check test parity build docs-test docs fuzz

install: ## Install Go dependencies and Python compatibility tooling
	go mod download
	uv sync
	uv run prek install

check: ## Check the Go implementation and retained Python oracle
	test -z "$$(gofmt -l *.go examples/chat/*.go)"
	go vet ./...
	uv run prek run --all-files
	uv run ty check

test: ## Run native Go behavior and race tests
	go test -race -cover ./...

parity: ## Compare Go with Python and run the original suite
	REPUBLIC_PYTHON_PARITY=1 go test ./...
	uv run pytest -q

build: ## Build Go packages and examples
	go build ./...

fuzz: ## Exercise parser and message decoder fuzz targets
	go test -run '^$$' -fuzz '^FuzzStreamParser$$' -fuzztime=10s
	go test -run '^$$' -fuzz '^FuzzMessageJSON$$' -fuzztime=10s

docs-test: ## Build documentation with strict checks
	uv run --frozen --group docs zensical build --clean --strict

docs: ## Serve documentation locally
	uv run --frozen --group docs zensical serve

.DEFAULT_GOAL := test
