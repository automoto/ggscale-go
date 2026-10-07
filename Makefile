.PHONY: build test test-verbose test-integration lint vet vet-js vulncheck tidy quickstart openapi-check

# Default target: run the same checks CI runs.
.DEFAULT_GOAL := check

check: lint vet vet-js test

# The gg-scale repository owns openapi.yaml; this repo keeps no copy.
# openapi-check runs TestOpenAPIOperationCoverage against the spec of the
# server tag SPEC_REF. It reads the network, so it is not part of `check`.
# Use a local spec with SPEC=../ggscale/openapi.yaml.
SPEC_REF ?= v0.9.71
SPEC ?= https://raw.githubusercontent.com/automoto/gg-scale/$(SPEC_REF)/openapi.yaml

openapi-check:
	@case "$(SPEC)" in \
	http*) tmp="$$(mktemp)" && trap 'rm -f "$$tmp"' EXIT && \
		curl -fsSL "$(SPEC)" -o "$$tmp" && \
		GGSCALE_SPEC="$$tmp" go test -count=1 -run TestOpenAPIOperationCoverage -v . ;; \
	*) GGSCALE_SPEC="$(abspath $(SPEC))" go test -count=1 -run TestOpenAPIOperationCoverage -v . ;; \
	esac

build:
	go build -o /dev/null ./...

test:
	go test -race ./...

test-verbose:
	go test -race -v ./...

# Spin up postgres + ggscale (pulled from GHCR) via docker compose,
# seed a tenant/project/API keys, run the -tags=integration tests, and
# tear the stack down. KEEP_STACK=1 leaves it running for debugging.
test-integration:
	./scripts/integration-test.sh

lint:
	golangci-lint run

vet:
	go vet ./...

# The browser build (GOOS=js) has its own realtime dial.
vet-js:
	GOOS=js GOARCH=wasm go vet ./...

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
