package transport

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
)

type proxySelector struct {
	configured    string
	configuredSet bool
	httpProxy     string
	httpsProxy    string
	allProxy      string
	noProxy       string

	mu   sync.Mutex
	user *url.Userinfo
}

func newProxySelector(s httpSettings) *proxySelector {
	return &proxySelector{
		configured:    s.proxy,
		configuredSet: s.proxySet,
		httpProxy:     firstEnv("http_proxy", "HTTP_PROXY"),
		httpsProxy:    firstEnv("https_proxy", "HTTPS_PROXY"),
		allProxy:      firstEnv("all_proxy", "ALL_PROXY"),
		noProxy:       firstEnv("no_proxy", "NO_PROXY"),
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func (p *proxySelector) rawFor(scheme string) string {
	if p.configuredSet {
		return p.configured
	}
	raw := p.httpProxy
	if scheme == string(SchemeHTTPS) {
		raw = p.httpsProxy
	}
	if raw == "" {
		raw = p.allProxy
	}
	return raw
}

func (p *proxySelector) proxy(req *http.Request) (*url.URL, error) {
	raw := p.rawFor(req.URL.Scheme)
	if raw == "" || noProxyMatches(p.noProxy, req.URL.Hostname()) {
		return nil, nil
	}
	u, err := parseProxyURL(raw)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.user != nil {
		u.User = p.user
	}
	return u, nil
}

func isSocksProxy(u *url.URL) bool {
	return strings.HasPrefix(u.Scheme, "socks5")
}

func (p *proxySelector) transportProxy(req *http.Request) (*url.URL, error) {
	u, err := p.proxy(req)
	if err != nil || u == nil || isSocksProxy(u) {
		return u, err
	}
	if req.URL.Scheme == string(SchemeHTTPS) {
		return nil, nil
	}
	stripped := *u
	stripped.User = nil
	return &stripped, nil
}

func (p *proxySelector) forURL(rawURL string) (*url.URL, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	u, err := p.proxy(&http.Request{URL: target})
	if err != nil || u == nil || isSocksProxy(u) {
		return nil, err
	}
	return u, nil
}

func (p *proxySelector) tunnelFor(addr string) (*url.URL, error) {
	if p.isTLSProxyAddress(addr) {
		return nil, nil
	}
	return p.forURL("https://" + addr)
}

func (p *proxySelector) isTLSProxyAddress(addr string) bool {
	raw := p.rawFor(string(SchemeHTTP))
	if raw == "" {
		return false
	}
	u, err := parseProxyURL(raw)
	return err == nil && u.Scheme == string(SchemeHTTPS) && u.Host == addr
}

func proxyResource(u *url.URL) string {
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

func (p *proxySelector) authenticate(user *url.Userinfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.user = user
}

func parseProxyURL(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: the proxy address cannot be parsed", ErrInvalidProxy)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("%w: scheme %q", ErrInvalidProxy, u.Scheme)
	}
	u.Scheme = scheme
	if u.Port() == "" {
		port := "1080"
		if scheme == "https" {
			port = "443"
		}
		u.Host = net.JoinHostPort(u.Hostname(), port)
	}
	return u, nil
}

func noProxyMatches(list, host string) bool {
	host = strings.ToLower(host)
	for entry := range strings.FieldsFuncSeq(strings.ToLower(list), isNoProxySeparator) {
		if entry == "*" {
			return true
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			addr, err := netip.ParseAddr(host)
			if err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		entry = strings.TrimPrefix(entry, ".")
		if name, _, err := net.SplitHostPort(entry); err == nil {
			entry = name
		}
		entry = strings.Trim(entry, "[]")
		if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}

func isNoProxySeparator(r rune) bool {
	return r == ',' || r == ' '
}
