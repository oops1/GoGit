package credential

import (
	"bytes"
	"context"
	"strconv"
	"strings"
)

const (
	libsecretSchema      = "org.git.Password"
	libsecretContentType = "text/plain"
)

type libsecretHelper struct {
	name string
	open func() (keyring, error)
}

func (h *libsecretHelper) Name() string { return h.name }

func (h *libsecretHelper) Get(ctx context.Context, q Query) (Answer, bool, error) {
	ans, found, err := h.lookup(ctx, q)
	if err != nil || !found || ans.Username == "" {
		ans.Wipe()
		return Answer{}, false, err
	}
	return ans, true, nil
}

func (h *libsecretHelper) lookup(ctx context.Context, q Query) (Answer, bool, error) {
	if q.Protocol == "" || (q.Host == "" && q.Path == "") {
		return Answer{}, false, nil
	}
	items, err := searchKeyring(ctx, h.open, libsecretAttributes(q))
	if err != nil || len(items) == 0 {
		return Answer{}, false, err
	}
	defer wipeKeyringItems(items)
	username := q.Username
	if user, ok := items[0].attributes["user"]; ok {
		username = user
	}
	password, _, _ := bytes.Cut(items[0].secret, []byte{'\n'})
	return Answer{Username: username, Password: bytes.Clone(password)}, true, nil
}

func (h *libsecretHelper) Store(ctx context.Context, q Query, a Answer) error {
	q = q.withAnswer(a)
	if q.Protocol == "" || (q.Host == "" && q.Path == "") || q.Username == "" || len(a.Password) == 0 {
		return nil
	}
	k, err := h.open()
	if err != nil {
		return err
	}
	defer k.close()
	attrs := libsecretAttributes(q)
	attrs[secretSchemaAttribute] = libsecretSchema
	return k.store(ctx, libsecretLabel(q), attrs, a.Password, libsecretContentType)
}

func (h *libsecretHelper) Erase(ctx context.Context, q Query, a Answer) error {
	q = q.withAnswer(a)
	if q.Protocol == "" && q.Host == "" && q.Path == "" && q.Username == "" {
		return nil
	}
	if len(a.Password) > 0 {
		existing, found, err := h.lookup(ctx, q)
		if err != nil {
			return err
		}
		differs := found && !secretsEqual(existing.Password, a.Password)
		existing.Wipe()
		if differs {
			return nil
		}
	}
	k, err := h.open()
	if err != nil {
		return err
	}
	defer k.close()
	items, err := k.search(ctx, libsecretAttributes(q))
	if err != nil {
		return err
	}
	defer wipeKeyringItems(items)
	return k.remove(ctx, items)
}

func libsecretAttributes(q Query) map[string]string {
	attrs := map[string]string{}
	host, port := libsecretHostPort(q.Host)
	for key, value := range map[string]string{"user": q.Username, "protocol": q.Protocol, "server": host, "object": q.Path} {
		if value != "" {
			attrs[key] = value
		}
	}
	if port != 0 {
		attrs["port"] = strconv.Itoa(int(port))
	}
	return attrs
}

func libsecretHostPort(host string) (string, uint16) {
	i := strings.LastIndexByte(host, ':')
	if i < 0 {
		return host, 0
	}
	digits := host[i+1:]
	end := 0
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	var port uint16
	for _, d := range digits[:end] {
		port = port*10 + uint16(d-'0')
	}
	return host[:i], port
}

func libsecretLabel(q Query) string {
	host, port := libsecretHostPort(q.Host)
	if port != 0 {
		host += ":" + strconv.Itoa(int(port))
	}
	return "Git: " + q.Protocol + "://" + host + "/" + q.Path
}
