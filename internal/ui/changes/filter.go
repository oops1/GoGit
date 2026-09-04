package changes

import "strings"

func FilterRows(rows []Row, query string) []Row {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return rows
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Name), q) || strings.Contains(strings.ToLower(r.RelPath), q) {
			out = append(out, r)
		}
	}
	return out
}
