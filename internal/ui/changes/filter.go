package changes

import "strings"

type StatusFilter string

const (
	FilterStaged    StatusFilter = "staged"
	FilterModified  StatusFilter = "modified"
	FilterAdded     StatusFilter = "added"
	FilterDeleted   StatusFilter = "deleted"
	FilterRenamed   StatusFilter = "renamed"
	FilterUntracked StatusFilter = "untracked"
	FilterIgnored   StatusFilter = "ignored"
	FilterConflict  StatusFilter = "conflict"
	FilterUnchanged StatusFilter = "unchanged"
)

var AllStatusFilters = []StatusFilter{
	FilterStaged,
	FilterModified,
	FilterAdded,
	FilterDeleted,
	FilterRenamed,
	FilterUntracked,
	FilterIgnored,
	FilterConflict,
	FilterUnchanged,
}

func FilterRows(rows []Row, query string) []Row {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return rows
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if rowMatchesQuery(r, q) {
			out = append(out, r)
		}
	}
	return out
}

func FilterRowsByStatus(rows []Row, query string, allowed map[StatusFilter]bool) []Row {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if q != "" && !rowMatchesQuery(r, q) {
			continue
		}
		if !rowVisibleForStatus(r, allowed) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func FilterRowsByDirectory(rows []Row, dir string) []Row {
	if dir == "" {
		return rows
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if rowInDirectory(r, dir) {
			out = append(out, r)
		}
	}
	return out
}

func rowInDirectory(r Row, dir string) bool {
	return r.RelDir == dir || strings.HasPrefix(r.RelDir, dir+"/")
}

func rowMatchesQuery(r Row, q string) bool {
	return strings.Contains(strings.ToLower(r.Name), q) || strings.Contains(strings.ToLower(r.RelPath), q)
}

func rowVisibleForStatus(r Row, allowed map[StatusFilter]bool) bool {
	for _, f := range rowStatusFilters(r) {
		if !allowed[f] {
			return false
		}
	}
	return true
}

func rowStatusFilters(r Row) []StatusFilter {
	filters := make([]StatusFilter, 0, 2)
	if f, ok := primaryStatusFilter(r.Status); ok {
		filters = append(filters, f)
	}
	if r.Status != RowConflict && r.IndexState != "" {
		filters = append(filters, FilterStaged)
	}
	return filters
}

func primaryStatusFilter(s RowStatus) (StatusFilter, bool) {
	switch s {
	case RowModified, RowTypeChanged:
		return FilterModified, true
	case RowAdded:
		return FilterAdded, true
	case RowDeleted:
		return FilterDeleted, true
	case RowRenamed, RowCopied:
		return FilterRenamed, true
	case RowUntracked:
		return FilterUntracked, true
	case RowIgnored:
		return FilterIgnored, true
	case RowConflict:
		return FilterConflict, true
	case RowUnchanged:
		return FilterUnchanged, true
	default:
		return "", false
	}
}

func NormalizeStatusFilters(names []string) []StatusFilter {
	seen := map[StatusFilter]bool{}
	result := make([]StatusFilter, 0, len(names))
	for _, name := range names {
		f := StatusFilter(strings.ToLower(strings.TrimSpace(name)))
		if seen[f] || !validStatusFilter(f) {
			continue
		}
		seen[f] = true
		result = append(result, f)
	}
	return result
}

func validStatusFilter(f StatusFilter) bool {
	for _, known := range AllStatusFilters {
		if known == f {
			return true
		}
	}
	return false
}

func AllowedStatusFilters(disabled []StatusFilter) map[StatusFilter]bool {
	disabledSet := make(map[StatusFilter]bool, len(disabled))
	for _, f := range disabled {
		disabledSet[f] = true
	}
	allowed := make(map[StatusFilter]bool, len(AllStatusFilters))
	for _, f := range AllStatusFilters {
		allowed[f] = !disabledSet[f]
	}
	return allowed
}

func DisabledStatusFilters(allowed map[StatusFilter]bool) []StatusFilter {
	disabled := make([]StatusFilter, 0, len(AllStatusFilters))
	for _, f := range AllStatusFilters {
		if !allowed[f] {
			disabled = append(disabled, f)
		}
	}
	return disabled
}
