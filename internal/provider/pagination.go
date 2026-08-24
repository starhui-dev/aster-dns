package provider

import (
	"errors"
	"unicode/utf8"
)

const (
	DefaultPageLimit  = 100
	MaximumPageLimit  = 1000
	MaximumCursorSize = 4096
)

func NormalizePageRequest(request PageRequest) (PageRequest, error) {
	if request.Limit < 0 || request.Limit > MaximumPageLimit {
		return PageRequest{}, errors.New("page limit is invalid")
	}
	if request.Limit == 0 {
		request.Limit = DefaultPageLimit
	}
	if len(request.Cursor) > MaximumCursorSize || !utf8.ValidString(request.Cursor) {
		return PageRequest{}, errors.New("page cursor is invalid")
	}
	return request, nil
}

func ValidatePage[T any](request PageRequest, page Page[T]) error {
	normalized, err := NormalizePageRequest(request)
	if err != nil {
		return err
	}
	if len(page.Items) > normalized.Limit {
		return errors.New("provider page exceeds the requested limit")
	}
	if page.NextCursor != "" {
		if len(page.NextCursor) > MaximumCursorSize || !utf8.ValidString(page.NextCursor) {
			return errors.New("provider returned an invalid cursor")
		}
		if page.NextCursor == normalized.Cursor {
			return errors.New("provider returned a non-advancing cursor")
		}
	}
	return nil
}
