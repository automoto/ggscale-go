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

// onlyScheme reports whether every security requirement of op is one of
// schemes alone.
func (op *specOperation) onlyScheme(schemes ...string) bool {
	if len(op.Security) == 0 {
		return false
	}
	for _, req := range op.Security {
		if len(req) != 1 {
			return false
		}
		for name := range req {
			if !slices.Contains(schemes, name) {
				return false
			}
		}
	}
	return true
}

// toolingSchemes guard the management and platform-admin APIs. They serve
// developer and operator tooling, not games, so the SDK does not wrap them.
var toolingSchemes = []string{"ManagementToken", "PlatformAdminToken"}

// TestOpenAPIOperationCoverage checks that each operationId in the server's
// openapi.yaml has an SDK wrapper, and that each secret-key operation is on
// the server client (server.go) and no other operation is only there.
// Operations guarded only by a tooling scheme are skipped. The spec path
// comes from GGSCALE_SPEC; make openapi-check sets it.
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

	count, skipped := 0, 0
	for path, item := range spec.Paths {
		for _, op := range []*specOperation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			if op.onlyScheme(toolingSchemes...) {
				skipped++
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
			if op.onlyScheme("SecretKey") {
				assert.Contains(t, files, "server.go", "secret-key operation %s must be on the server client", op.OperationID)
				continue
			}
			assert.False(t, slices.Equal(files, []string{"server.go"}),
				"publishable-key operation %s is only on the server client", op.OperationID)
		}
	}
	t.Logf("%d operations checked, %d tooling operations skipped", count, skipped)
	assert.Positive(t, count, "the spec has no operations")
}
