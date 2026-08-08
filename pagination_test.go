package ggscale

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCursorSequence_iterates_pages_and_stops(t *testing.T) {
	sequence := cursorSequence("", func(cursor string) ([]int, string, error) {
		switch cursor {
		case "":
			return []int{1, 2}, "next", nil
		case "next":
			return []int{3}, "", nil
		default:
			return nil, "", errors.New("unexpected cursor")
		}
	})

	var values []int
	for value, err := range sequence {
		require.NoError(t, err)
		values = append(values, value)
	}
	assert.Equal(t, []int{1, 2, 3}, values)
}

func TestCursorSequence_rejects_stuck_cursor(t *testing.T) {
	sequence := cursorSequence("same", func(string) ([]int, string, error) {
		return nil, "same", nil
	})
	for _, err := range sequence {
		assert.ErrorIs(t, err, errCursorDidNotAdvance)
		return
	}
	t.Fatal("expected pagination error")
}
