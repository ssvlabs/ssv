package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionalPagination_ToRequest(t *testing.T) {
	t.Parallel()

	opts := PaginationOptions{DefaultPerPage: 1000, MaxPerPage: 10000}

	t.Run("unset returns nil", func(t *testing.T) {
		t.Parallel()

		req, err := (OptionalPagination{}).ToRequest(opts)
		require.NoError(t, err)
		require.Nil(t, req)
	})

	t.Run("empty object defaults", func(t *testing.T) {
		t.Parallel()

		var p OptionalPagination
		require.NoError(t, json.Unmarshal([]byte(`{}`), &p))

		req, err := p.ToRequest(opts)
		require.NoError(t, err)
		require.Equal(t, uint64(1), req.Page)
		require.Equal(t, uint64(1000), req.PerPage)
	})

	t.Run("per_page only defaults page to 1", func(t *testing.T) {
		t.Parallel()

		var p OptionalPagination
		require.NoError(t, json.Unmarshal([]byte(`{"per_page":2}`), &p))

		req, err := p.ToRequest(opts)
		require.NoError(t, err)
		require.Equal(t, uint64(1), req.Page)
		require.Equal(t, uint64(2), req.PerPage)
	})

	t.Run("page only defaults per_page", func(t *testing.T) {
		t.Parallel()

		var p OptionalPagination
		require.NoError(t, json.Unmarshal([]byte(`{"page":2}`), &p))

		req, err := p.ToRequest(opts)
		require.NoError(t, err)
		require.Equal(t, uint64(2), req.Page)
		require.Equal(t, uint64(1000), req.PerPage)
	})

	t.Run("page 0 invalid when provided", func(t *testing.T) {
		t.Parallel()

		var p OptionalPagination
		require.NoError(t, json.Unmarshal([]byte(`{"page":0}`), &p))

		_, err := p.ToRequest(opts)
		require.Error(t, err)
	})

	t.Run("per_page 0 invalid when provided", func(t *testing.T) {
		t.Parallel()

		var p OptionalPagination
		require.NoError(t, json.Unmarshal([]byte(`{"per_page":0}`), &p))

		_, err := p.ToRequest(opts)
		require.Error(t, err)
	})

	t.Run("per_page too large invalid", func(t *testing.T) {
		t.Parallel()

		var p OptionalPagination
		require.NoError(t, json.Unmarshal([]byte(`{"per_page":10001}`), &p))

		_, err := p.ToRequest(opts)
		require.Error(t, err)
	})

	t.Run("null treated as unset", func(t *testing.T) {
		t.Parallel()

		var p OptionalPagination
		require.NoError(t, json.Unmarshal([]byte(`null`), &p))

		req, err := p.ToRequest(opts)
		require.NoError(t, err)
		require.Nil(t, req)
	})
}

func TestPaginationRequest_SliceBounds(t *testing.T) {
	t.Parallel()

	t.Run("per_page larger than total", func(t *testing.T) {
		t.Parallel()

		p := PaginationRequest{Page: 1, PerPage: 10}
		start, end := p.SliceBounds(5)
		require.Equal(t, uint64(0), start)
		require.Equal(t, uint64(5), end)
	})

	t.Run("page beyond total returns empty", func(t *testing.T) {
		t.Parallel()

		p := PaginationRequest{Page: 100, PerPage: 2}
		start, end := p.SliceBounds(5)
		require.Equal(t, uint64(5), start)
		require.Equal(t, uint64(5), end)
	})

	t.Run("very large page clamps via overflow protection", func(t *testing.T) {
		t.Parallel()

		p := PaginationRequest{Page: ^uint64(0), PerPage: 1000}
		start, end := p.SliceBounds(5)
		require.Equal(t, uint64(5), start)
		require.Equal(t, uint64(5), end)
	})

	t.Run("total zero always empty", func(t *testing.T) {
		t.Parallel()

		p := PaginationRequest{Page: 1, PerPage: 2}
		start, end := p.SliceBounds(0)
		require.Equal(t, uint64(0), start)
		require.Equal(t, uint64(0), end)
	})
}

func TestPaginationFromRequest(t *testing.T) {
	t.Parallel()

	t.Run("total zero", func(t *testing.T) {
		t.Parallel()

		out := PaginationFromRequest(PaginationRequest{Page: 1, PerPage: 10}, 0)
		require.Equal(t, uint64(0), out.TotalPages)
	})

	t.Run("ceil division", func(t *testing.T) {
		t.Parallel()

		out := PaginationFromRequest(PaginationRequest{Page: 1, PerPage: 2}, 5)
		require.Equal(t, uint64(3), out.TotalPages)
	})

	t.Run("no overflow for very large totals", func(t *testing.T) {
		t.Parallel()

		maxUint64 := ^uint64(0)
		out := PaginationFromRequest(PaginationRequest{Page: 1, PerPage: 2}, maxUint64)
		require.Equal(t, (maxUint64/2)+1, out.TotalPages)
	})
}
