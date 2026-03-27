package domain

// PaginationFilter is used for list endpoints that support page/limit query params.
//
// Rules:
//   - Default limit : 10
//   - limit = 0     : fetch ALL records (no cap)
//   - page  < 1     : treated as page 1
type PaginationFilter struct {
	Page  int // 1-based; default 1
	Limit int // default 10; 0 = all
}

func (f *PaginationFilter) Offset() int {
	if f.Page < 1 {
		f.Page = 1
	}
	return (f.Page - 1) * f.Limit
}

// SafeLimit returns the actual limit to pass to GORM.
// 0 = all (caller should skip the .Limit() call), >0 = explicit page size.
func (f *PaginationFilter) SafeLimit() int {
	if f.Limit < 0 {
		return 10 // treat negative as default
	}
	return f.Limit // 0 = all, >0 = explicit limit
}

// IsAll reports whether the caller requested all records (limit == 0).
func (f *PaginationFilter) IsAll() bool {
	return f.Limit == 0
}