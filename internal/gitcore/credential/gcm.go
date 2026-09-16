package credential

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

const gistGitHubHost = "gist.github.com"

type gcmStore interface {
	get(ctx context.Context, service, account string) (Answer, bool, error)
	put(ctx context.Context, service, account string, secret []byte) error
	remove(ctx context.Context, service, account string, secret []byte) error
}

type managerHelper struct {
	name  string
	store gcmStore
}

func (h *managerHelper) Name() string { return h.name }

func (h *managerHelper) Get(ctx context.Context, q Query) (Answer, bool, error) {
	service := gcmServiceName(q)
	if service == "" {
		return Answer{}, false, nil
	}
	return h.store.get(ctx, service, q.Username)
}

func (h *managerHelper) Store(ctx context.Context, q Query, a Answer) error {
	q = q.withAnswer(a)
	service := gcmServiceName(q)
	if service == "" || (q.Username == "" && len(a.Password) == 0) {
		return nil
	}
	return h.store.put(ctx, service, q.Username, a.Password)
}

func (h *managerHelper) Erase(ctx context.Context, q Query, a Answer) error {
	q = q.withAnswer(a)
	service := gcmServiceName(q)
	if service == "" {
		return nil
	}
	return h.store.remove(ctx, service, q.Username, a.Password)
}

func gcmServiceName(q Query) string {
	if q.Protocol == "" || q.Host == "" {
		return ""
	}
	scheme := strings.ToLower(q.Protocol)
	hostParts := strings.Split(q.Host, ":")
	host := strings.ToLower(hostParts[0])
	if host == gistGitHubHost {
		return "https://github.com"
	}
	u := url.URL{Scheme: scheme, Host: host}
	if len(hostParts) > 1 {
		if port, err := strconv.Atoi(hostParts[1]); err == nil && port != gcmDefaultPort(scheme) {
			u.Host = host + ":" + strconv.Itoa(port)
		}
	}
	if q.Path != "" {
		u.Path = "/" + strings.TrimPrefix(q.Path, "/")
	}
	return strings.TrimRight(u.String(), "/")
}

func gcmDefaultPort(scheme string) int {
	switch scheme {
	case "http":
		return 80
	case "https":
		return 443
	}
	return -1
}

type gcmURI struct {
	scheme string
	host   string
	port   int
	path   string
}

func (u gcmURI) defaultPort() bool {
	return u.port == gcmDefaultPort(u.scheme)
}

func parseGCMURI(raw string) (gcmURI, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return gcmURI{}, false
	}
	parsed := gcmURI{
		scheme: strings.ToLower(u.Scheme),
		host:   strings.ToLower(u.Hostname()),
		path:   u.EscapedPath(),
	}
	parsed.port = gcmDefaultPort(parsed.scheme)
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return gcmURI{}, false
		}
		parsed.port = port
	}
	if parsed.path == "" {
		parsed.path = "/"
	}
	return parsed, true
}
