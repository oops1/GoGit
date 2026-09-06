package credential

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type storeEntry struct {
	raw      string
	parsed   bool
	protocol string
	host     string
	path     string
	username string
	password string
}

type storeHelper struct {
	name string
	path string
	mu   sync.Mutex
}

func (h *storeHelper) Name() string { return h.name }

func (h *storeHelper) Get(_ context.Context, q Query) (Answer, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entries, err := readStoreFile(h.path)
	if err != nil {
		return Answer{}, false, err
	}
	best := -1
	for i := range entries {
		e := &entries[i]
		if !e.parsed || !storeEntryMatches(e, q) {
			continue
		}
		if best < 0 || len(e.path) >= len(entries[best].path) {
			best = i
		}
	}
	if best < 0 {
		return Answer{}, false, nil
	}
	return Answer{Username: entries[best].username, Password: []byte(entries[best].password)}, true, nil
}

func (h *storeHelper) Store(_ context.Context, q Query, a Answer) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	entries, err := readStoreFile(h.path)
	if err != nil {
		return err
	}
	kept := make([]storeEntry, 0, len(entries)+1)
	for _, e := range entries {
		if e.parsed && e.protocol == q.Protocol && e.host == q.Host && e.path == q.Path && e.username == q.Username {
			continue
		}
		kept = append(kept, e)
	}
	kept = append(kept, storeEntry{
		raw:      formatStoreLine(q, a.Password),
		parsed:   true,
		protocol: q.Protocol,
		host:     q.Host,
		path:     q.Path,
		username: q.Username,
		password: string(a.Password),
	})
	return writeStoreFile(h.path, kept)
}

func (h *storeHelper) Erase(_ context.Context, q Query) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	entries, err := readStoreFile(h.path)
	if err != nil {
		return err
	}
	kept := make([]storeEntry, 0, len(entries))
	for _, e := range entries {
		if e.parsed && storeEntryMatches(&e, q) {
			continue
		}
		kept = append(kept, e)
	}
	return writeStoreFile(h.path, kept)
}

func storeEntryMatches(e *storeEntry, q Query) bool {
	if e.protocol != q.Protocol || e.host != q.Host {
		return false
	}
	if q.Username != "" && e.username != q.Username {
		return false
	}
	return strings.HasPrefix(q.Path, e.path)
}

func readStoreFile(path string) ([]storeEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []storeEntry
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		e, ok := parseStoreLine(line)
		e.raw = line
		e.parsed = ok
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseStoreLine(raw string) (storeEntry, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return storeEntry{}, false
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return storeEntry{}, false
	}
	e := storeEntry{
		protocol: strings.ToLower(u.Scheme),
		host:     u.Host,
		path:     strings.TrimPrefix(u.Path, "/"),
	}
	if u.User != nil {
		e.username = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			e.password = pw
		}
	}
	return e, true
}

func formatStoreLine(q Query, password []byte) string {
	u := &url.URL{Scheme: q.Protocol, Host: q.Host}
	if q.Path != "" {
		u.Path = "/" + q.Path
	}
	switch {
	case len(password) > 0:
		u.User = url.UserPassword(q.Username, string(password))
	case q.Username != "":
		u.User = url.User(q.Username)
	}
	return u.String()
}

func writeStoreFile(path string, entries []storeEntry) error {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.raw)
		b.WriteByte('\n')
	}
	return writeAtomicSecret(path, []byte(b.String()))
}

func writeAtomicSecret(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
