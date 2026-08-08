package ggscale

import (
	"errors"
	"iter"
)

var errCursorDidNotAdvance = errors.New("ggscale: pagination cursor did not advance")

func cursorSequence[T any](initialCursor string, fetch func(string) ([]T, string, error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		cursor := initialCursor
		for {
			items, next, err := fetch(cursor)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
			if next == "" {
				return
			}
			if next == cursor {
				var zero T
				yield(zero, errCursorDidNotAdvance)
				return
			}
			cursor = next
		}
	}
}
