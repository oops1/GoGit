package credential

import (
	"context"
	"strconv"
	"strings"
)

const gcmLegacyGenericPrefix = "LegacyGeneric:target="

type gcmWindowsStore struct {
	manager   credentialManager
	namespace string
}

func (s *gcmWindowsStore) get(_ context.Context, service, account string) (Answer, bool, error) {
	cred, found, err := s.first(service, account)
	if err != nil || !found {
		return Answer{}, false, err
	}
	defer clear(cred.blob)
	return Answer{Username: cred.userName, Password: utf8FromUTF16LE(cred.blob)}, true, nil
}

func (s *gcmWindowsStore) put(_ context.Context, service, account string, secret []byte) error {
	target := gcmTargetName(s.namespace, service, "")
	existing, found, err := s.manager.read(target)
	if err != nil {
		return err
	}
	if found {
		stored := utf8FromUTF16LE(existing.blob)
		unchanged := secretsEqual(stored, secret)
		clear(stored)
		clear(existing.blob)
		switch {
		case existing.userName != account:
			target = gcmTargetName(s.namespace, service, account)
		case unchanged:
			return nil
		}
	}
	blob := utf16LEFromUTF8(secret)
	defer clear(blob)
	return s.manager.write(winCredential{target: target, userName: account, blob: blob})
}

func (s *gcmWindowsStore) remove(_ context.Context, service, account string, secret []byte) error {
	cred, found, err := s.first(service, account)
	if err != nil || !found {
		return err
	}
	defer clear(cred.blob)
	if len(secret) > 0 {
		stored := utf8FromUTF16LE(cred.blob)
		same := secretsEqual(stored, secret)
		clear(stored)
		if !same {
			return nil
		}
	}
	return s.manager.remove(gcmTrimLegacyPrefix(cred.target))
}

func (s *gcmWindowsStore) first(service, account string) (winCredential, bool, error) {
	creds, err := s.manager.enumerate("")
	if err != nil {
		return winCredential{}, false, err
	}
	defer wipeWinCredentials(creds)
	for _, c := range creds {
		if gcmWindowsMatches(s.namespace, service, account, c) {
			c.blob = append([]byte(nil), c.blob...)
			return c, true, nil
		}
	}
	return winCredential{}, false, nil
}

func gcmTrimLegacyPrefix(target string) string {
	if i := strings.Index(target, gcmLegacyGenericPrefix); i >= 0 {
		return target[i+len(gcmLegacyGenericPrefix):]
	}
	return target
}

func gcmWindowsMatches(namespace, service, account string, c winCredential) bool {
	if strings.TrimSpace(account) != "" && account != c.userName {
		return false
	}
	target := gcmTrimLegacyPrefix(c.target)
	if namespace != "" {
		rest, ok := strings.CutPrefix(target, namespace+":")
		if !ok {
			return false
		}
		target = rest
	}
	if service == target {
		return true
	}
	serviceURI, ok := parseGCMURI(service)
	if !ok {
		return false
	}
	targetURI, ok := parseGCMURI(target)
	return ok &&
		serviceURI.scheme == targetURI.scheme &&
		serviceURI.host == targetURI.host &&
		serviceURI.port == targetURI.port &&
		strings.EqualFold(serviceURI.path, targetURI.path)
}

func gcmTargetName(namespace, service, account string) string {
	var b strings.Builder
	if namespace != "" {
		b.WriteString(namespace)
		b.WriteByte(':')
	}
	u, ok := parseGCMURI(service)
	if !ok {
		b.WriteString(service)
		return b.String()
	}
	b.WriteString(u.scheme)
	b.WriteString("://")
	if strings.TrimSpace(account) != "" {
		b.WriteString(strings.ReplaceAll(account, "@", "_"))
		b.WriteByte('@')
	}
	b.WriteString(u.host)
	if !u.defaultPort() {
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(u.port))
	}
	b.WriteString(strings.TrimRight(u.path, "/"))
	return b.String()
}
