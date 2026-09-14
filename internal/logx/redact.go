package logx

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

const redactedValue = "***"

var defaultSecretKeyParts = []string{
	"password",
	"passwd",
	"pwd",
	"passphrase",
	"token",
	"secret",
	"authorization",
	"credential",
	"apikey",
	"privatekey",
	"cookie",
}

const exactSecretKey = "pat"

var keyNormalizer = strings.NewReplacer("-", "", "_", "", ".", "")

var authHeaderPattern = regexp.MustCompile(`(?i)((?:proxy-)?authorization|(?:set-)?cookie)(["']?[ \t]*[:=][ \t]*["']?)(?:([a-z]+(?:-[a-z0-9]+)*)([ \t]+))?[^\r\n\]}]+`)

var userinfoURLPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*)://([^/\s"'<>]*)@`)

var secretPairPattern = regexp.MustCompile(`([A-Za-z0-9_.\-]+)["']?[ \t]*(?:=[ \t]*["']?|:(?:[ \t]*["'])?)`)

const pairValueTerminators = " \t\r\n,}])&\"';"

type redactHandler struct {
	next  slog.Handler
	parts []string
}

func Redact(next slog.Handler, keys ...string) slog.Handler {
	parts := append([]string(nil), defaultSecretKeyParts...)
	for _, k := range keys {
		if n := normalizeKey(k); n != "" {
			parts = append(parts, n)
		}
	}
	return &redactHandler{next: next, parts: parts}
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, RedactText(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, nr)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = h.redactAttr(a)
	}
	return &redactHandler{next: h.next.WithAttrs(out), parts: h.parts}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{next: h.next.WithGroup(name), parts: h.parts}
}

func (h *redactHandler) redactAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		out := make([]slog.Attr, len(group))
		for i, ga := range group {
			out[i] = h.redactAttr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	if isSecretKey(a.Key, h.parts) {
		return slog.String(a.Key, redactedValue)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, RedactText(a.Value.String()))
	case slog.KindAny:
		return slog.String(a.Key, RedactText(anyText(a.Value.Any())))
	}
	return a
}

func anyText(v any) string {
	switch v := v.(type) {
	case []byte:
		return string(v)
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	}
	return fmt.Sprintf("%+v", v)
}

func normalizeKey(key string) string {
	return keyNormalizer.Replace(strings.ToLower(key))
}

func isSecretKey(key string, parts []string) bool {
	n := normalizeKey(key)
	if n == exactSecretKey {
		return true
	}
	for _, p := range parts {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

func RedactText(s string) string {
	s = authHeaderPattern.ReplaceAllString(s, "${1}${2}${3}${4}"+redactedValue)
	s = userinfoURLPattern.ReplaceAllStringFunc(s, redactUserinfo)
	return redactSecretPairs(s)
}

func redactUserinfo(match string) string {
	sub := userinfoURLPattern.FindStringSubmatch(match)
	scheme, userinfo := sub[1], sub[2]
	if user, _, ok := strings.Cut(userinfo, ":"); ok {
		return scheme + "://" + user + ":" + redactedValue + "@"
	}
	lower := strings.ToLower(scheme)
	if strings.HasSuffix(lower, "http") || strings.HasSuffix(lower, "https") {
		return scheme + "://" + redactedValue + "@"
	}
	return match
}

func redactSecretPairs(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range secretPairPattern.FindAllStringSubmatchIndex(s, -1) {
		key := s[m[2]:m[3]]
		if m[0] < last || maskedByHeaderPattern(key) || !isSecretKey(key, defaultSecretKeyParts) {
			continue
		}
		end := secretValueEnd(s, m[1])
		if end == m[1] {
			continue
		}
		b.WriteString(s[last:m[1]])
		b.WriteString(redactedValue)
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

func maskedByHeaderPattern(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasSuffix(lower, "authorization") || strings.HasSuffix(lower, "cookie")
}

func secretValueEnd(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case strings.IndexByte("[{(", c) >= 0:
			depth++
		case depth > 0 && strings.IndexByte("]})", c) >= 0:
			depth--
		case depth == 0 && strings.IndexByte(pairValueTerminators, c) >= 0:
			return i
		}
	}
	return len(s)
}
