package transport

import "strings"

const hiddenPassword = "***"

func SafeURL(raw string) string {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return raw
	}
	authority := rest
	if idx := strings.IndexAny(rest, "/?#"); idx >= 0 {
		authority = rest[:idx]
	}
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return raw
	}
	user, _, hasPassword := strings.Cut(authority[:at], ":")
	if !hasPassword {
		return raw
	}
	return scheme + "://" + user + ":" + hiddenPassword + "@" + rest[at+1:]
}
