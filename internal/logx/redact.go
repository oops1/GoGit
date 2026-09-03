package logx

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

const redactedValue = "***"

var defaultRedactKeys = []string{"password", "passphrase", "token", "secret", "authorization", "credential"}

var authHeaderPattern = regexp.MustCompile(`(?i)(authorization:\s*(?:basic|bearer)\s+)\S+`)

var credentialURLPattern = regexp.MustCompile(`://([^/@:\s]+):([^/@\s]+)@`)

type redactHandler struct {
	next slog.Handler
	keys map[string]struct{}
}

func Redact(next slog.Handler, keys ...string) slog.Handler {
	set := make(map[string]struct{}, len(defaultRedactKeys)+len(keys))
	for _, k := range defaultRedactKeys {
		set[k] = struct{}{}
	}
	for _, k := range keys {
		set[strings.ToLower(k)] = struct{}{}
	}
	return &redactHandler{next: next, keys: set}
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, redactText(r.Message), r.PC)
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
	return &redactHandler{next: h.next.WithAttrs(out), keys: h.keys}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{next: h.next.WithGroup(name), keys: h.keys}
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
	if h.isSecretKey(a.Key) {
		return slog.String(a.Key, redactedValue)
	}
	if a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, redactText(a.Value.String()))
	}
	return a
}

func (h *redactHandler) isSecretKey(key string) bool {
	_, ok := h.keys[strings.ToLower(key)]
	return ok
}

func redactText(s string) string {
	s = authHeaderPattern.ReplaceAllString(s, "${1}"+redactedValue)
	s = credentialURLPattern.ReplaceAllString(s, "://$1:"+redactedValue+"@")
	return s
}
