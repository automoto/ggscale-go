package ggscale

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenAPIV094OperationCoverage prevents a newly generated operation from
// silently landing without a runtime wrapper. The manifest is generated from
// the pinned openapi.yaml snapshot by make openapi-generate.
func TestOpenAPIV094OperationCoverage(t *testing.T) {
	manifest, err := os.ReadFile("testdata/openapi-v0.9.4-operations.txt")
	require.NoError(t, err)

	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	var source strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Clean(name))
		require.NoError(t, readErr)
		source.Write(raw)
	}

	seen := map[string]bool{}
	count := 0
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		require.Len(t, fields, 3, "malformed operation manifest line %q", line)
		operationID := fields[0]
		assert.False(t, seen[operationID], "duplicate operationId %s", operationID)
		seen[operationID] = true
		assert.Contains(t, source.String(), `"`+operationID+`"`,
			"OpenAPI operation %s (%s %s) has no SDK wrapper", operationID, fields[1], fields[2])
		count++
	}
	assert.Equal(t, 70, count, "unexpected v0.9.4 operation count")
}
