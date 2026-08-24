package provider

import "testing"

func TestPaginationNormalizationAndValidation(t *testing.T) {
	t.Parallel()
	normalized, err := NormalizePageRequest(PageRequest{})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized.Limit != DefaultPageLimit {
		t.Fatalf("default limit = %d", normalized.Limit)
	}
	if err = ValidatePage(normalized, Page[int]{Items: make([]int, normalized.Limit), NextCursor: "next"}); err != nil {
		t.Fatalf("validate page: %v", err)
	}
}

func TestPaginationRejectsInvalidBoundsAndCursor(t *testing.T) {
	t.Parallel()
	if _, err := NormalizePageRequest(PageRequest{Limit: MaximumPageLimit + 1}); err == nil {
		t.Fatal("oversized limit passed")
	}
	request := PageRequest{Cursor: "same", Limit: 1}
	if err := ValidatePage(request, Page[int]{Items: []int{1}, NextCursor: "same"}); err == nil {
		t.Fatal("non-advancing cursor passed")
	}
	if err := ValidatePage(PageRequest{Limit: 1}, Page[int]{Items: []int{1, 2}}); err == nil {
		t.Fatal("oversized page passed")
	}
}
