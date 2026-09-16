package credential

import (
	"strconv"
	"strings"
)

const (
	urlAlphaDigit  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	urlSchemeChars = urlAlphaDigit + "+.-"
	urlHostChars   = urlAlphaDigit + ".-_[:]"
	urlUnsafeChars = " <>\"%{}|\\^`"
	urlReserved    = ":/?#[]@!$&'()*+,;="
	urlEncodeChars = urlUnsafeChars + ":?#[]@!$&'()*+,;="
	upperHexDigits = "0123456789ABCDEF"
)

type urlInfo struct {
	scheme  string
	hasUser bool
	user    string
	host    string
	port    string
	path    string
}

func spanOf(s, set string) int {
	for i := range len(s) {
		if strings.IndexByte(set, s[i]) < 0 {
			return i
		}
	}
	return len(s)
}

func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func decodeHexPair(s string) (byte, bool) {
	hi, okHi := hexValue(s[0])
	lo, okLo := hexValue(s[1])
	return hi<<4 | lo, okHi && okLo
}

func appendEscaped(b []byte, c byte) []byte {
	return append(b, '%', upperHexDigits[c>>4], upperHexDigits[c&0x0F])
}

func normalizeURLEscapes(from string) (string, bool) {
	var b []byte
	for i := 0; i < len(from); {
		c := from[i]
		i++
		escaped := false
		if c == '%' {
			if len(from)-i < 2 {
				return "", false
			}
			decoded, ok := decodeHexPair(from[i : i+2])
			if !ok {
				return "", false
			}
			c, escaped = decoded, true
			i += 2
		}
		if c <= 0x1F || c >= 0x7F || strings.IndexByte(urlUnsafeChars, c) >= 0 || (escaped && strings.IndexByte(urlReserved, c) >= 0) {
			b = appendEscaped(b, c)
			continue
		}
		b = append(b, c)
	}
	return string(b), true
}

func normalizeURL(raw string, allowGlobs bool) (urlInfo, bool) {
	spanned := spanOf(raw, urlSchemeChars)
	if spanned == 0 || !strings.ContainsRune(urlAlphaDigit[:52], rune(raw[0])) || !strings.HasPrefix(raw[spanned:], "://") {
		return urlInfo{}, false
	}
	info := urlInfo{scheme: strings.ToLower(raw[:spanned])}
	rest := raw[spanned+3:]
	slash := len(rest)
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		slash = i
	}
	if at := strings.IndexByte(rest, '@'); at >= 0 && at < slash {
		info.hasUser = true
		userinfo, ok := normalizeURLEscapes(rest[:at])
		if !ok {
			return urlInfo{}, false
		}
		info.user, _, _ = strings.Cut(userinfo, ":")
		rest = rest[at+1:]
		slash -= at + 1
	}
	hasHost := rest != "" && strings.IndexByte(":/?#", rest[0]) < 0
	if !hasHost && info.scheme != "file" {
		return urlInfo{}, false
	}
	colon := slash
	if slash > 0 {
		c := slash - 1
		for c > 0 && rest[c] != ':' && rest[c] != ']' {
			c--
		}
		if rest[c] == ':' {
			colon = c
		}
	}
	if !hasHost && colon+1 < slash {
		return urlInfo{}, false
	}
	hostChars := urlHostChars
	if allowGlobs {
		hostChars += "*"
	}
	if spanOf(rest, hostChars) < colon {
		return urlInfo{}, false
	}
	info.host = strings.ToLower(rest[:colon])
	if colon < slash {
		port, ok := normalizeURLPort(info.scheme, rest[colon+1:slash])
		if !ok {
			return urlInfo{}, false
		}
		info.port = port
	}
	path, ok := normalizeURLPath(rest[slash:])
	if !ok {
		return urlInfo{}, false
	}
	info.path = path
	return info, true
}

func normalizeURLPort(scheme, port string) (string, bool) {
	trimmed := strings.TrimLeft(port, "0")
	if trimmed == "" && port != "" {
		trimmed = "0"
	}
	switch {
	case trimmed == "", scheme == "http" && trimmed == "80", scheme == "https" && trimmed == "443":
		return "", true
	case spanOf(trimmed, urlAlphaDigit[52:]) < len(trimmed) || len(trimmed) > 5:
		return "", false
	}
	n, _ := strconv.Atoi(trimmed)
	return trimmed, n > 0 && n <= 65535
}

func normalizeURLPath(rest string) (string, bool) {
	b := []byte{'/'}
	rest = strings.TrimPrefix(rest, "/")
	for {
		segmentStart := len(b)
		next := len(rest)
		if i := strings.IndexAny(rest, "/?#"); i >= 0 {
			next = i
		}
		segment, ok := normalizeURLEscapes(rest[:next])
		if !ok {
			return "", false
		}
		b = append(b, segment...)
		skipSlash := false
		switch string(b[segmentStart:]) {
		case ".":
			if segmentStart == 1 {
				b, skipSlash = b[:len(b)-1], true
			} else {
				b = b[:len(b)-2]
			}
		case "..":
			previous := len(b) - 3
			if previous == 0 {
				return "", false
			}
			previous--
			for b[previous] != '/' {
				previous--
			}
			if previous == 0 {
				b, skipSlash = b[:1], true
			} else {
				b = b[:previous]
			}
		}
		rest = rest[next:]
		if rest == "" || rest[0] != '/' {
			break
		}
		rest = rest[1:]
		if !skipSlash {
			b = append(b, '/')
		}
	}
	tail, ok := normalizeURLEscapes(rest)
	return string(b) + tail, ok
}

func urlMatches(u, pattern urlInfo) bool {
	if u.scheme != pattern.scheme {
		return false
	}
	if pattern.hasUser && (!u.hasUser || u.user != pattern.user) {
		return false
	}
	return urlHostMatches(u.host, pattern.host) && u.port == pattern.port && urlPathMatches(u.path, pattern.path)
}

func urlHostMatches(host, pattern string) bool {
	for host != "" && pattern != "" {
		hostPart, hostRest, _ := strings.Cut(host, ".")
		patternPart, patternRest, _ := strings.Cut(pattern, ".")
		if patternPart != "*" && patternPart != hostPart {
			return false
		}
		host, pattern = hostRest, patternRest
	}
	return host == "" && pattern == ""
}

func urlPathMatches(path, prefix string) bool {
	if prefix == "" || prefix == "/" {
		return path == "" || path[0] == '/'
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	return len(path) == len(prefix) || path[len(prefix)] == '/'
}

func percentEncode(s string, encodeSlash, hostAndPort bool) string {
	var b []byte
	for i := range len(s) {
		c := s[i]
		var escape bool
		switch {
		case c <= 0x1F || c >= 0x7F:
			escape = true
		case c == '/' && encodeSlash:
			escape = true
		case hostAndPort:
			escape = strings.IndexByte(urlAlphaDigit+"-.:[]", c) < 0
		default:
			escape = strings.IndexByte(urlEncodeChars, c) >= 0
		}
		if escape {
			b = appendEscaped(b, c)
		} else {
			b = append(b, c)
		}
	}
	return string(b)
}

func credentialURL(q Query) string {
	var b strings.Builder
	b.WriteString(q.Protocol)
	b.WriteString("://")
	if q.Username != "" {
		b.WriteString(percentEncode(q.Username, true, false))
		b.WriteByte('@')
	}
	b.WriteString(percentEncode(q.Host, false, true))
	if q.Path != "" {
		b.WriteByte('/')
		b.WriteString(percentEncode(q.Path, false, false))
	}
	return b.String()
}

type partialCredential struct {
	protocol, host, path, username         string
	hasProtocol, hasHost, hasPath, hasUser bool
}

func urlDecode(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if c, ok := decodeHexPair(s[i+1 : i+3]); ok {
				b = append(b, c)
				i += 2
				continue
			}
		}
		b = append(b, s[i])
	}
	return string(b)
}

func parsePartialCredentialURL(raw string) (partialCredential, bool) {
	var p partialCredential
	rest := raw
	if i := strings.Index(raw, "://"); i >= 0 {
		rest = raw[i+3:]
		if i > 0 {
			p.protocol, p.hasProtocol = raw[:i], true
		}
	}
	slash := len(rest)
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		slash = i
	}
	host := rest[:slash]
	password := ""
	if at := strings.IndexByte(rest, '@'); at >= 0 && at < slash {
		userinfo := rest[:at]
		if colon := strings.IndexByte(rest, ':'); colon >= 0 && colon < at {
			userinfo, password = rest[:colon], urlDecode(rest[colon+1:at])
		}
		p.username, p.hasUser = urlDecode(userinfo), true
		host = rest[at+1 : slash]
	}
	if host != "" {
		p.host, p.hasHost = urlDecode(host), true
	}
	path := strings.TrimLeft(rest[slash:], "/")
	if path != "" {
		decoded := urlDecode(path)
		trimmed := strings.TrimRight(decoded, "/")
		if trimmed == "" {
			trimmed = decoded[:1]
		}
		p.path, p.hasPath = trimmed, true
	}
	for _, part := range []string{p.username, password, p.protocol, p.host, p.path} {
		if strings.Contains(part, "\n") {
			return partialCredential{}, false
		}
	}
	return p, true
}

func (p partialCredential) matches(q Query) bool {
	check := func(has bool, want, have string) bool {
		return !has || (have != "" && want == have)
	}
	return check(p.hasProtocol, p.protocol, q.Protocol) &&
		check(p.hasHost, p.host, q.Host) &&
		check(p.hasPath, p.path, q.Path) &&
		check(p.hasUser, p.username, q.Username)
}
