.PHONY: build test test-verbose test-integration lint vet vulncheck tidy quickstart openapi-generate openapi-check

# Default target: run the same checks CI runs.
.DEFAULT_GOAL := check

check: openapi-check lint vet test

openapi-generate:
	go run ./internal/cmd/openapi-operations -spec openapi.yaml -output testdata/openapi-v0.9.4-operations.txt

openapi-check:
	go run ./internal/cmd/openapi-operations -spec openapi.yaml -output testdata/openapi-v0.9.4-operations.txt -check

build:
	go build -o /dev/null ./...

test:
	go test -race ./...

test-verbose:
	go test -race -v ./...

# Spin up postgres + ggscale (pulled from Docker Hub) via docker compose,
# seed a tenant/project/API keys, run the -tags=integration tests, and
# tear the stack down. KEEP_STACK=1 leaves it running for debugging.
test-integration:
	./scripts/integration-test.sh

lint:
	golangci-lint run

vet:
	go vet ./...

vulncheck:
	go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

tidy:
	go mod tidy

# Run the quickstart against a ggscale-server reachable at $$BASE
# (default http://localhost:8080). Requires GGSCALE_API_KEY set to a
# key minted via the control panel.
quickstart:
	@test -n "$$GGSCALE_API_KEY" || (echo "GGSCALE_API_KEY must be set" && exit 1)
	go run ./examples/quickstart
