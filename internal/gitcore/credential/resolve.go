package credential

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

const (
	gcmDefaultNamespace = "git"
	gcmStoreWincredman  = "wincredman"
	gcmStoreSecret      = "secretservice"
	gcmStorePlaintext   = "plaintext"
	windowsExeSuffix    = ".exe"
)

func resolveHelper(raw string, env helperEnvironment) (Helper, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "!") {
		return nil, ErrUnsupportedHelper
	}
	fields := strings.Fields(trimmed)
	switch fields[0] {
	case "cache":
		return nil, ErrUnsupportedHelper
	case "store":
		return newStoreHelper(trimmed, fields[1:])
	}
	switch helperProgramName(trimmed) {
	case "manager", "manager-core":
		return newManagerHelper(trimmed, env)
	case "wincred":
		return newWincredHelper(trimmed)
	case "libsecret":
		return newLibsecretHelper(trimmed)
	}
	return nil, ErrUnsupportedHelper
}

func helperProgramName(spec string) string {
	i := strings.LastIndexAny(spec, `/\`)
	if i < 0 {
		return spec
	}
	base := spec[i+1:]
	if cut := len(base) - len(windowsExeSuffix); cut > 0 && strings.EqualFold(base[cut:], windowsExeSuffix) {
		base = base[:cut]
	}
	name, ok := strings.CutPrefix(base, "git-credential-")
	if !ok {
		return ""
	}
	return name
}

func newWincredHelper(name string) (Helper, error) {
	if windowsCredentials == nil {
		return nil, ErrUnsupportedHelper
	}
	return &wincredHelper{name: name, manager: windowsCredentials}, nil
}

func newLibsecretHelper(name string) (Helper, error) {
	if openKeyring == nil {
		return nil, ErrUnsupportedHelper
	}
	return &libsecretHelper{name: name, open: openKeyring}, nil
}

func newManagerHelper(name string, env helperEnvironment) (Helper, error) {
	storeName := strings.ToLower(env.setting("GCM_CREDENTIAL_STORE", "credentialstore"))
	if storeName == "" {
		storeName = gcmDefaultStore
	}
	namespace := env.setting("GCM_NAMESPACE", "namespace")
	if namespace == "" {
		namespace = gcmDefaultNamespace
	}
	switch storeName {
	case gcmStoreWincredman:
		if windowsCredentials == nil {
			return nil, ErrUnsupportedHelper
		}
		return &managerHelper{name: name, store: &gcmWindowsStore{manager: windowsCredentials, namespace: namespace}}, nil
	case gcmStoreSecret:
		if openKeyring == nil {
			return nil, ErrUnsupportedHelper
		}
		return &managerHelper{name: name, store: &gcmKeyringStore{open: openKeyring, namespace: namespace}}, nil
	case gcmStorePlaintext:
		root, err := gcmPlaintextRoot(env)
		if err != nil {
			return nil, err
		}
		return &managerHelper{name: name, store: &gcmFileStore{root: root, namespace: namespace}}, nil
	}
	return nil, ErrUnsupportedHelper
}

func gcmPlaintextRoot(env helperEnvironment) (string, error) {
	if configured := env.setting("GCM_PLAINTEXT_STORE_PATH", "plaintextstorepath"); configured != "" {
		return config.ExpandPath(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gcm", "store"), nil
}

func newStoreHelper(raw string, args []string) (Helper, error) {
	path := ""
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--file="); ok {
			path = v
		}
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".git-credentials")
	} else {
		expanded, err := config.ExpandPath(path)
		if err != nil {
			return nil, err
		}
		path = expanded
	}
	return &storeHelper{name: raw, path: path}, nil
}
