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

// Pagination is the resolved (non-optional) pagination configuration.
// It is only constructible outside this package via PaginationRequest.ToPagination.
type Pagination interface {
	Page() uint64
	PerPage() uint64
	// SliceBounds returns safe [start,end) bounds for slicing a collection with the given total length.
	SliceBounds(total uint64) (start, end uint64)
}

type PaginationRequest struct {
	Set bool `json:"-"`

	Page    *uint64 `json:"page,omitempty"`
	PerPage *uint64 `json:"per_page,omitempty"`
}

func (p *PaginationRequest) Bind(value string) error {
	if value == "" {
		return nil
	}
	return fmt.Errorf("pagination must be provided in request body")
}

func (p *PaginationRequest) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*p = PaginationRequest{}
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

type pagination struct {
	page    uint64
	perPage uint64
}

func (p pagination) Page() uint64    { return p.page }
func (p pagination) PerPage() uint64 { return p.perPage }

func (p pagination) SliceBounds(total uint64) (start, end uint64) {
	if p.page == 0 || p.perPage == 0 {
		// unreachable by construction
		panic("invalid pagination state with page=0 or per_page=0")
	}

	pageIndex := p.page - 1
	hi, start := bits.Mul64(pageIndex, p.perPage)
	if hi != 0 || start > total {
		start = total
	}

	end, carry := bits.Add64(start, p.perPage, 0)
	if carry != 0 || end > total {
		end = total
	}

	return start, end
}

func (p PaginationRequest) ToPagination(opts PaginationOptions) (Pagination, error) {
	if !p.Set {
		return nil, nil
	}

	var page uint64
	var perPage uint64

	if p.Page != nil {
		if *p.Page == 0 {
			return nil, fmt.Errorf("page must be >= 1")
		}
		page = *p.Page
	}
	if p.PerPage != nil {
		if *p.PerPage == 0 {
			return nil, fmt.Errorf("per_page must be >= 1")
		}
		perPage = *p.PerPage
	}

	if page == 0 {
		page = 1
	}
	if perPage == 0 {
		if opts.DefaultPerPage == 0 {
			return nil, fmt.Errorf("default per_page must be >= 1")
		}
		perPage = opts.DefaultPerPage
	}
	if opts.MaxPerPage > 0 && perPage > opts.MaxPerPage {
		return nil, fmt.Errorf("per_page must be <= %d", opts.MaxPerPage)
	}

	return pagination{page: page, perPage: perPage}, nil
}

type PaginationResponse struct {
	Page       uint64 `json:"page"`
	PerPage    uint64 `json:"per_page"`
	Total      uint64 `json:"total"`
	TotalPages uint64 `json:"total_pages"`
}

func PaginationResponseFromPagination(p Pagination, total uint64) PaginationResponse {
	if p.Page() == 0 || p.PerPage() == 0 {
		// unreachable by construction
		panic("invalid pagination state with page=0 or per_page=0")
	}

	out := PaginationResponse{
		Page:    p.Page(),
		PerPage: p.PerPage(),
		Total:   total,
	}
	if total > 0 {
		out.TotalPages = total / p.PerPage()
		if total%p.PerPage() != 0 {
			out.TotalPages++
		}
	}
	return out
}
