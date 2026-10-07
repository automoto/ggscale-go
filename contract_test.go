package ggscale

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type specOperation struct {
	OperationID string                `yaml:"operationId"`
	Security    []map[string][]string `yaml:"security"`
}

type specPathItem struct {
	Get    *specOperation `yaml:"get"`
	Post   *specOperation `yaml:"post"`
	Put    *specOperation `yaml:"put"`
	Patch  *specOperation `yaml:"patch"`
	Delete *specOperation `yaml:"delete"`
}

// secretOnly reports whether every security requirement of op is the secret
// key alone.
func (op *specOperation) secretOnly() bool {
	if len(op.Security) == 0 {
		return false
	}
	for _, req := range op.Security {
		if _, ok := req["SecretKey"]; !ok || len(req) != 1 {
			return false
		}
	}
	return true
}

// TestOpenAPIOperationCoverage checks that each operationId in the server's
// openapi.yaml has an SDK wrapper, and that each secret-key operation is on
// the server client (server.go) and no other operation is only there. The
// spec path comes from GGSCALE_SPEC; make openapi-check sets it.
func TestOpenAPIOperationCoverage(t *testing.T) {
	specPath := os.Getenv("GGSCALE_SPEC")
	if specPath == "" {
		t.Skip("GGSCALE_SPEC is not set; run make openapi-check")
	}
	raw, err := os.ReadFile(filepath.Clean(specPath))
	require.NoError(t, err)
	var spec struct {
		Paths map[string]specPathItem `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &spec))

	sources := map[string]string{}
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, readErr := os.ReadFile(filepath.Clean(name))
		require.NoError(t, readErr)
		sources[name] = string(src)
	}

	count := 0
	for path, item := range spec.Paths {
		for _, op := range []*specOperation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			count++
			var files []string
			for name, src := range sources {
				if strings.Contains(src, `"`+op.OperationID+`"`) {
					files = append(files, name)
				}
			}
			if !assert.NotEmpty(t, files, "operation %s (%s) has no SDK wrapper", op.OperationID, path) {
				continue
			}
			if op.secretOnly() {
				assert.Contains(t, files, "server.go", "secret-key operation %s must be on the server client", op.OperationID)
				continue
			}
			assert.False(t, slices.Equal(files, []string{"server.go"}),
				"publishable-key operation %s is only on the server client", op.OperationID)
		}
	}
	assert.Positive(t, count, "the spec has no operations")
}
