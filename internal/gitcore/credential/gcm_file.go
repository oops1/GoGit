package credential

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const gcmFileExtension = ".credential"

var (
	readGCMFile = os.ReadFile
	readGCMDir  = os.ReadDir
)

type gcmFileStore struct {
	root      string
	namespace string
}

type gcmFileCredential struct {
	path     string
	service  string
	account  string
	password []byte
}

func wipeGCMFileCredentials(creds []gcmFileCredential) {
	for i := range creds {
		clear(creds[i].password)
	}
}

func (s *gcmFileStore) get(_ context.Context, service, account string) (Answer, bool, error) {
	creds, err := s.enumerate(service, account)
	if err != nil || len(creds) == 0 {
		return Answer{}, false, err
	}
	defer wipeGCMFileCredentials(creds)
	return Answer{Username: creds[0].account, Password: bytes.Clone(creds[0].password)}, true, nil
}

func (s *gcmFileStore) put(_ context.Context, service, account string, secret []byte) error {
	if strings.ContainsAny(account, `/\`) || account == "." || account == ".." {
		return ErrInvalidAccount
	}
	creds, err := s.enumerate(service, account)
	if err != nil {
		return err
	}
	unchanged := len(creds) > 0 && creds[0].account == account && secretsEqual(creds[0].password, secret)
	wipeGCMFileCredentials(creds)
	if unchanged {
		return nil
	}
	var content bytes.Buffer
	content.Write(secret)
	content.WriteString(gcmFileNewline + "service=" + service + gcmFileNewline + "account=" + account + gcmFileNewline)
	defer clear(content.Bytes())
	return writeAtomicSecret(filepath.Join(s.serviceDirectory(service), account+gcmFileExtension), content.Bytes())
}

func (s *gcmFileStore) remove(_ context.Context, service, account string, secret []byte) error {
	creds, err := s.enumerate(service, account)
	if err != nil || len(creds) == 0 {
		return err
	}
	defer wipeGCMFileCredentials(creds)
	if len(secret) > 0 && !secretsEqual(creds[0].password, secret) {
		return nil
	}
	return os.Remove(creds[0].path)
}

func (s *gcmFileStore) serviceDirectory(service string) string {
	separator := string(filepath.Separator)
	var slug strings.Builder
	if s.namespace != "" {
		slug.WriteString(s.namespace + separator)
	}
	u, ok := parseGCMURI(service)
	if !ok {
		slug.WriteString(service)
		return filepath.Join(s.root, slug.String())
	}
	slug.WriteString(u.scheme + separator + u.host)
	if !u.defaultPort() {
		slug.WriteString(gcmPortSeparator + strconv.Itoa(u.port))
	}
	slug.WriteString(strings.ReplaceAll(u.path, "/", separator))
	return filepath.Join(s.root, slug.String())
}

func (s *gcmFileStore) enumerate(service, account string) ([]gcmFileCredential, error) {
	dir := s.serviceDirectory(service)
	entries, err := readGCMDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	anyAccount := strings.TrimSpace(account) == ""
	var out []gcmFileCredential
	for _, entry := range entries {
		fileAccount, ok := strings.CutSuffix(entry.Name(), gcmFileExtension)
		if !ok || entry.IsDir() || (!anyAccount && !strings.EqualFold(account, fileAccount)) {
			continue
		}
		cred, ok, err := readGCMFileCredential(filepath.Join(dir, entry.Name()))
		if err != nil {
			wipeGCMFileCredentials(out)
			return nil, err
		}
		if ok && strings.EqualFold(service, cred.service) && (anyAccount || strings.EqualFold(account, cred.account)) {
			out = append(out, cred)
			continue
		}
		clear(cred.password)
	}
	return out, nil
}

func readGCMFileCredential(path string) (gcmFileCredential, bool, error) {
	data, err := readGCMFile(path)
	if err != nil {
		return gcmFileCredential{}, false, err
	}
	defer clear(data)
	end := bytes.Index(data, []byte(gcmFileNewline))
	if end <= 0 {
		return gcmFileCredential{}, false, nil
	}
	cred := gcmFileCredential{path: path}
	hasService := false
	for line := range strings.Lines(string(data[end+len(gcmFileNewline):])) {
		key, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "=")
		switch {
		case !ok:
		case strings.EqualFold(key, "service"):
			cred.service, hasService = value, true
		case strings.EqualFold(key, "account"):
			cred.account = value
		}
	}
	if !hasService {
		return gcmFileCredential{}, false, nil
	}
	cred.password = bytes.Clone(data[:end])
	return cred, true, nil
}
