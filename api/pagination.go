package api

import (
	"encoding/json"
	"fmt"
	"math/bits"
)

type PaginationOptions struct {
	DefaultPerPage uint64
	MaxPerPage     uint64
}

type OptionalPagination struct {
	Set bool `json:"-"`

	Page    *uint64 `json:"page,omitempty"`
	PerPage *uint64 `json:"per_page,omitempty"`
}

func (p *OptionalPagination) Bind(value string) error {
	if value == "" {
		return nil
	}
	return fmt.Errorf("pagination must be provided in request body")
}

func (p *OptionalPagination) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*p = OptionalPagination{}
		return nil
	}

	var tmp struct {
		Page    *uint64 `json:"page"`
		PerPage *uint64 `json:"per_page"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	p.Set = true
	p.Page = tmp.Page
	p.PerPage = tmp.PerPage
	return nil
}

func (p OptionalPagination) ToRequest(opts PaginationOptions) (*PaginationRequest, error) {
	if !p.Set {
		return nil, nil
	}

	var req PaginationRequest

	if p.Page != nil {
		if *p.Page == 0 {
			return nil, fmt.Errorf("page must be >= 1")
		}
		req.Page = *p.Page
	}
	if p.PerPage != nil {
		if *p.PerPage == 0 {
			return nil, fmt.Errorf("per_page must be >= 1")
		}
		req.PerPage = *p.PerPage
	}

	if req.Page == 0 {
		req.Page = 1
	}
	if req.PerPage == 0 {
		req.PerPage = opts.DefaultPerPage
	}
	if opts.MaxPerPage > 0 && req.PerPage > opts.MaxPerPage {
		return nil, fmt.Errorf("per_page must be <= %d", opts.MaxPerPage)
	}

	return &req, nil
}

type PaginationRequest struct {
	Page    uint64
	PerPage uint64
}

type Pagination struct {
	Page       uint64 `json:"page"`
	PerPage    uint64 `json:"per_page"`
	Total      uint64 `json:"total"`
	TotalPages uint64 `json:"total_pages"`
}

// SliceBounds returns safe [start,end) bounds for slicing a collection with the given total length.
func (p PaginationRequest) SliceBounds(total uint64) (start, end uint64) {
	if p.Page == 0 || p.PerPage == 0 {
		return 0, 0
	}

	pageIndex := p.Page - 1
	hi, start := bits.Mul64(pageIndex, p.PerPage)
	if hi != 0 || start > total {
		start = total
	}

	end, carry := bits.Add64(start, p.PerPage, 0)
	if carry != 0 || end > total {
		end = total
	}

	return start, end
}

func PaginationFromRequest(p PaginationRequest, total uint64) Pagination {
	out := Pagination{
		Page:    p.Page,
		PerPage: p.PerPage,
		Total:   total,
	}
	if total > 0 && p.PerPage > 0 {
		out.TotalPages = total / p.PerPage
		if total%p.PerPage != 0 {
			out.TotalPages++
		}
	}
	return out
}
