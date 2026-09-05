package transport

import (
	"fmt"
	stdurl "net/url"
	"strings"
)

type Scheme string

const (
	SchemeHTTP  Scheme = "http"
	SchemeHTTPS Scheme = "https"
	SchemeGit   Scheme = "git"
	SchemeSSH   Scheme = "ssh"
	SchemeFile  Scheme = "file"
)

type Endpoint struct {
	Scheme Scheme
	User   string
	Host   string
	Port   string
	Path   string
}

func (e Endpoint) IsLocal() bool {
	return e.Scheme == SchemeFile
}

const redactedPassword = "transport.Password{REDACTED}"

type Password []byte

func (p Password) String() string {
	return redactedPassword
}

func (p Password) Bytes() []byte {
	return p
}

func (p *Password) Wipe() {
	for i := range *p {
		(*p)[i] = 0
	}
	*p = nil
}

func ParseURL(raw string) (Endpoint, Password, error) {
	if raw == "" {
		return Endpoint{}, nil, fmt.Errorf("%w: the url is empty", ErrInvalidURL)
	}
	if hasWindowsDrivePrefix(raw) {
		return Endpoint{Scheme: SchemeFile, Path: raw}, nil, nil
	}
	if strings.Contains(raw, "://") {
		return parseSchemeURL(raw)
	}
	if idx := strings.IndexByte(raw, ':'); idx >= 0 && !strings.ContainsAny(raw[:idx], "/\\") {
		if endpoint, ok := parseSCPLike(raw); ok {
			return endpoint, nil, nil
		}
	}
	return Endpoint{Scheme: SchemeFile, Path: raw}, nil, nil
}

func hasWindowsDrivePrefix(s string) bool {
	return len(s) >= 3 && isASCIILetter(s[0]) && s[1] == ':' && (s[2] == '\\' || s[2] == '/')
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func parseSCPLike(raw string) (Endpoint, bool) {
	rest := raw
	user := ""
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		candidate := rest[:at]
		if candidate != "" && !strings.ContainsAny(candidate, "/\\:[]") {
			user = candidate
			rest = rest[at+1:]
		}
	}
	host, path, ok := splitSCPHostPath(rest)
	if !ok {
		return Endpoint{}, false
	}
	return Endpoint{Scheme: SchemeSSH, User: user, Host: host, Path: path}, true
}

func splitSCPHostPath(rest string) (host, path string, ok bool) {
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return "", "", false
		}
		host = rest[1:end]
		tail := rest[end+1:]
		if !strings.HasPrefix(tail, ":") {
			return "", "", false
		}
		path = tail[1:]
	} else {
		idx := strings.IndexByte(rest, ':')
		if idx < 0 {
			return "", "", false
		}
		host = rest[:idx]
		path = rest[idx+1:]
	}
	if host == "" || strings.ContainsAny(host, "/\\") {
		return "", "", false
	}
	return host, path, true
}

func parseSchemeURL(raw string) (Endpoint, Password, error) {
	parsed, err := stdurl.Parse(raw)
	if err != nil {
		return Endpoint{}, nil, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	scheme := Scheme(strings.ToLower(parsed.Scheme))
	switch scheme {
	case SchemeHTTP, SchemeHTTPS, SchemeGit, SchemeSSH:
		return endpointFromAuthorityURL(scheme, parsed)
	case SchemeFile:
		return endpointFromFileURL(parsed), nil, nil
	default:
		return Endpoint{}, nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, parsed.Scheme)
	}
}

func endpointFromAuthorityURL(scheme Scheme, parsed *stdurl.URL) (Endpoint, Password, error) {
	if parsed.Host == "" {
		return Endpoint{}, nil, fmt.Errorf("%w: %q has no host", ErrInvalidURL, parsed.Redacted())
	}
	var user string
	var password Password
	if parsed.User != nil {
		user = parsed.User.Username()
		if pw, ok := parsed.User.Password(); ok {
			password = Password(pw)
		}
	}
	return Endpoint{
		Scheme: scheme,
		User:   user,
		Host:   parsed.Hostname(),
		Port:   parsed.Port(),
		Path:   parsed.Path,
	}, password, nil
}

func endpointFromFileURL(parsed *stdurl.URL) Endpoint {
	path := parsed.Path
	if path == "" {
		path = parsed.Opaque
	}
	if trimmed := strings.TrimPrefix(path, "/"); hasWindowsDrivePrefix(trimmed) {
		path = trimmed
	}
	return Endpoint{Scheme: SchemeFile, Path: path}
}
