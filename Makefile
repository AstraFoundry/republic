.PHONY: install check test parity build docs-test docs fuzz

install: ## Download Go dependencies
	go mod download

check: ## Check Go formatting, static analysis and patch whitespace
	test -z "$$(gofmt -l *.go examples/chat/*.go)"
	go vet ./...
	git diff --check

test: ## Run Go behavior, compatibility and race tests
	go test -race -cover ./...

parity: ## Check the frozen original-behavior baselines using Go only
	go test -run 'Test(Request|Response)Compatibility|TestRecordedProtocols' ./...

build: ## Build Go packages and examples
	go build ./...

fuzz: ## Exercise parser and message decoder fuzz targets
	go test -run '^$$' -fuzz '^FuzzStreamParser$$' -fuzztime=10s
	go test -run '^$$' -fuzz '^FuzzMessageJSON$$' -fuzztime=10s

docs-test: ## Build documentation using the external Zensical tool
	uvx --from zensical==0.0.69 zensical build --clean --strict

docs: ## Preview documentation using the external Zensical tool
	uvx --from zensical==0.0.69 zensical serve

.DEFAULT_GOAL := test
