package credential

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"testing"
)

type fakeCredentialManager struct {
	creds        []winCredential
	enumerateErr error
	readErr      error
	writeErr     error
	removeErr    error
	filters      []string
	removed      []string
}

func (m *fakeCredentialManager) enumerate(filter string) ([]winCredential, error) {
	m.filters = append(m.filters, filter)
	if m.enumerateErr != nil {
		return nil, m.enumerateErr
	}
	prefix, wildcard := strings.CutSuffix(filter, "*")
	var out []winCredential
	for _, c := range m.creds {
		if filter == "" || (wildcard && strings.HasPrefix(c.target, prefix)) || c.target == filter {
			c.blob = append([]byte(nil), c.blob...)
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *fakeCredentialManager) read(target string) (winCredential, bool, error) {
	if m.readErr != nil {
		return winCredential{}, false, m.readErr
	}
	for _, c := range m.creds {
		if strings.EqualFold(c.target, target) {
			c.blob = append([]byte(nil), c.blob...)
			return c, true, nil
		}
	}
	return winCredential{}, false, nil
}

func (m *fakeCredentialManager) write(cred winCredential) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	cred.blob = append([]byte(nil), cred.blob...)
	for i, c := range m.creds {
		if strings.EqualFold(c.target, cred.target) {
			m.creds[i] = cred
			return nil
		}
	}
	m.creds = append(m.creds, cred)
	return nil
}

func (m *fakeCredentialManager) remove(target string) error {
	m.removed = append(m.removed, target)
	if m.removeErr != nil {
		return m.removeErr
	}
	for i, c := range m.creds {
		if strings.EqualFold(gcmTrimLegacyPrefix(c.target), target) {
			m.creds = append(m.creds[:i], m.creds[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *fakeCredentialManager) add(target, user, password string) {
	m.creds = append(m.creds, winCredential{target: target, userName: user, blob: utf16LEFromUTF8([]byte(password))})
}

type fakeKeyringItem struct {
	attributes map[string]string
	secret     []byte
	label      string
	content    string
}

type fakeKeyring struct {
	items     []fakeKeyringItem
	openErr   error
	searchErr error
	storeErr  error
	removeErr error
	opens     int
	closes    int
	searches  []map[string]string
}

func (k *fakeKeyring) opener() func() (keyring, error) {
	return func() (keyring, error) {
		k.opens++
		if k.openErr != nil {
			return nil, k.openErr
		}
		return k, nil
	}
}

func (k *fakeKeyring) search(_ context.Context, attrs map[string]string) ([]keyringItem, error) {
	k.searches = append(k.searches, maps.Clone(attrs))
	if k.searchErr != nil {
		return nil, k.searchErr
	}
	var out []keyringItem
	for i, item := range k.items {
		if item.attributes != nil && attributesContain(item.attributes, attrs) {
			out = append(out, keyringItem{handle: strconv.Itoa(i), attributes: maps.Clone(item.attributes), secret: append([]byte(nil), item.secret...)})
		}
	}
	return out, nil
}

func attributesContain(have, want map[string]string) bool {
	for key, value := range want {
		if have[key] != value {
			return false
		}
	}
	return true
}

func (k *fakeKeyring) store(_ context.Context, label string, attrs map[string]string, secret []byte, contentType string) error {
	if k.storeErr != nil {
		return k.storeErr
	}
	item := fakeKeyringItem{attributes: maps.Clone(attrs), secret: append([]byte(nil), secret...), label: label, content: contentType}
	for i, existing := range k.items {
		if maps.Equal(existing.attributes, attrs) {
			k.items[i] = item
			return nil
		}
	}
	k.items = append(k.items, item)
	return nil
}

func (k *fakeKeyring) remove(_ context.Context, items []keyringItem) error {
	if k.removeErr != nil {
		return k.removeErr
	}
	for _, item := range items {
		i, _ := strconv.Atoi(item.handle)
		k.items[i].attributes = nil
	}
	return nil
}

func (k *fakeKeyring) close() {
	k.closes++
}

func (k *fakeKeyring) live() []fakeKeyringItem {
	var out []fakeKeyringItem
	for _, item := range k.items {
		if item.attributes != nil {
			out = append(out, item)
		}
	}
	return out
}

func stubPlatformStores(t *testing.T, manager credentialManager, open func() (keyring, error)) {
	t.Helper()
	prevManager, prevOpen := windowsCredentials, openKeyring
	windowsCredentials, openKeyring = manager, open
	t.Cleanup(func() { windowsCredentials, openKeyring = prevManager, prevOpen })
}
