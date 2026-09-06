package credential

import (
	"strings"

	"github.com/oops1/gogit/internal/gitcore/transport"
)

func ParseQuery(rawURL string, useHTTPPath bool) (Query, error) {
	endpoint, _, err := transport.ParseURL(rawURL)
	if err != nil {
		return Query{}, err
	}
	q := Query{
		Protocol: string(endpoint.Scheme),
		Host:     endpoint.Host,
		Username: endpoint.User,
	}
	if endpoint.Port != "" {
		q.Host += ":" + endpoint.Port
	}
	if useHTTPPath {
		q.Path = strings.TrimPrefix(endpoint.Path, "/")
	}
	return q, nil
}
