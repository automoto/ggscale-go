.PHONY: build test test-verbose lint vet vulncheck tidy quickstart

# Default target: run the same checks CI runs.
.DEFAULT_GOAL := check

check: lint vet test

build:
	go build -o /dev/null ./...

test:
	go test -race ./...

test-verbose:
	go test -race -v ./...

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
# key minted via the dashboard.
quickstart:
	@test -n "$$GGSCALE_API_KEY" || (echo "GGSCALE_API_KEY must be set" && exit 1)
	go run ./examples/quickstart
