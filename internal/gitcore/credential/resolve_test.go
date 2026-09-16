package credential

import (
	"errors"
	"path/filepath"
	"testing"
)

func testEnvironment(t *testing.T, content string, vars map[string]string) helperEnvironment {
	t.Helper()
	applied, err := applyCredentialConfig(loadTestConfig(t, content), "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	return helperEnvironment{
		settings: applied.settings,
		getenv:   func(name string) string { return vars[name] },
	}
}

func emptyEnvironment(t *testing.T) helperEnvironment {
	t.Helper()
	return testEnvironment(t, "", nil)
}

func TestResolveHelperRejectsHelpersThatNeedAProcess(t *testing.T) {
	stubPlatformStores(t, &fakeCredentialManager{}, (&fakeKeyring{}).opener())
	for _, spec := range []string{
		"",
		"   ",
		"!true",
		`!C:\Program Files\Git\mingw64\bin\git-credential-manager.exe`,
		"cache",
		"cache --timeout=60",
		"osxkeychain",
		"foo --with-arg",
		"manager --no-ui",
		"/usr/local/bin/my-helper",
		`C:\Tools\git-credential-custom.exe`,
		"git-credential-manager",
	} {
		if _, err := resolveHelper(spec, emptyEnvironment(t)); !errors.Is(err, ErrUnsupportedHelper) {
			t.Errorf("resolveHelper(%q) = %v, want ErrUnsupportedHelper", spec, err)
		}
	}
}

func TestHelperProgramNameReadsPlainNamesAndHelperPaths(t *testing.T) {
	cases := map[string]string{
		"manager": "manager",
		`C:\Program Files\Git\mingw64\bin\git-credential-manager.exe`:              "manager",
		"C:/Program Files/Git/mingw64/libexec/git-core/git-credential-wincred.EXE": "wincred",
		"/usr/local/bin/git-credential-manager":                                    "manager",
		"/usr/share/doc/git/contrib/credential/libsecret/git-credential-libsecret": "libsecret",
		"/opt/git-credential-manager-core":                                         "manager-core",
		"/usr/bin/helper":                                                          "",
		"/usr/bin/.exe":                                                            "",
	}
	for spec, want := range cases {
		if got := helperProgramName(spec); got != want {
			t.Errorf("helperProgramName(%q) = %q, want %q", spec, got, want)
		}
	}
}

func TestResolveHelperWincredUsesTheCredentialManager(t *testing.T) {
	manager := &fakeCredentialManager{}
	stubPlatformStores(t, manager, nil)
	for _, spec := range []string{"wincred", `C:\Program Files\Git\mingw64\libexec\git-core\git-credential-wincred.exe`} {
		h, err := resolveHelper(spec, emptyEnvironment(t))
		if err != nil {
			t.Fatalf("resolveHelper(%q) returned %v", spec, err)
		}
		wh, ok := h.(*wincredHelper)
		if !ok || wh.manager != manager || wh.Name() != spec {
			t.Fatalf("resolveHelper(%q) = %#v", spec, h)
		}
	}
}

func TestResolveHelperWincredIsUnsupportedWithoutTheCredentialManager(t *testing.T) {
	stubPlatformStores(t, nil, (&fakeKeyring{}).opener())
	if _, err := resolveHelper("wincred", emptyEnvironment(t)); !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("err = %v, want ErrUnsupportedHelper", err)
	}
}

func TestResolveHelperLibsecretUsesTheKeyring(t *testing.T) {
	stubPlatformStores(t, nil, (&fakeKeyring{}).opener())
	h, err := resolveHelper("libsecret", emptyEnvironment(t))
	if err != nil {
		t.Fatal(err)
	}
	if lh, ok := h.(*libsecretHelper); !ok || lh.Name() != "libsecret" || lh.open == nil {
		t.Fatalf("resolveHelper(libsecret) = %#v", h)
	}
}

func TestResolveHelperLibsecretIsUnsupportedWithoutAKeyring(t *testing.T) {
	stubPlatformStores(t, &fakeCredentialManager{}, nil)
	if _, err := resolveHelper("libsecret", emptyEnvironment(t)); !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("err = %v, want ErrUnsupportedHelper", err)
	}
}

func managerStore(t *testing.T, spec string, env helperEnvironment) gcmStore {
	t.Helper()
	h, err := resolveHelper(spec, env)
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	mh, ok := h.(*managerHelper)
	if !ok {
		t.Fatalf("resolveHelper returned %T, want *managerHelper", h)
	}
	return mh.store
}

func TestResolveHelperManagerPicksTheConfiguredStore(t *testing.T) {
	manager := &fakeCredentialManager{}
	stubPlatformStores(t, manager, (&fakeKeyring{}).opener())
	root := t.TempDir()

	store := managerStore(t, "manager", testEnvironment(t, "[credential]\n\tcredentialStore = WinCredMan\n", nil))
	if ws, ok := store.(*gcmWindowsStore); !ok || ws.manager != manager || ws.namespace != "git" {
		t.Fatalf("wincredman store = %#v", store)
	}

	store = managerStore(t, "manager-core", testEnvironment(t, "[credential]\n\tcredentialStore = secretservice\n\tnamespace = work\n", nil))
	if ks, ok := store.(*gcmKeyringStore); !ok || ks.namespace != "work" || ks.open == nil {
		t.Fatalf("secretservice store = %#v", store)
	}

	env := testEnvironment(t, "[credential]\n\tcredentialStore = secretservice\n", map[string]string{
		"GCM_CREDENTIAL_STORE":     "plaintext",
		"GCM_PLAINTEXT_STORE_PATH": root,
		"GCM_NAMESPACE":            "env",
	})
	store = managerStore(t, "manager", env)
	if fs, ok := store.(*gcmFileStore); !ok || fs.root != root || fs.namespace != "env" {
		t.Fatalf("plaintext store = %#v", store)
	}
}

func TestResolveHelperManagerHonoursURLScopedStoreSettings(t *testing.T) {
	stubPlatformStores(t, &fakeCredentialManager{}, (&fakeKeyring{}).opener())
	content := "[credential]\n\tcredentialStore = secretservice\n" +
		"[credential \"https://example.com\"]\n\tcredentialStore = plaintext\n\tplaintextStorePath = " + filepath.ToSlash(t.TempDir()) + "\n" +
		"[credential \"https://other.example\"]\n\tcredentialStore = wincredman\n"
	store := managerStore(t, "manager", testEnvironment(t, content, nil))
	if _, ok := store.(*gcmFileStore); !ok {
		t.Fatalf("store = %T, want the store scoped to the remote url", store)
	}
}

func TestResolveHelperManagerFallsBackToThePlatformDefaultStore(t *testing.T) {
	stubPlatformStores(t, &fakeCredentialManager{}, (&fakeKeyring{}).opener())
	if gcmDefaultStore == "" {
		if _, err := resolveHelper("manager", emptyEnvironment(t)); !errors.Is(err, ErrUnsupportedHelper) {
			t.Fatalf("err = %v, want ErrUnsupportedHelper without a configured store", err)
		}
		return
	}
	if store, ok := managerStore(t, "manager", emptyEnvironment(t)).(*gcmWindowsStore); !ok {
		t.Fatalf("default store = %#v", store)
	}
}

func TestResolveHelperManagerRejectsStoresThatAreNotAvailable(t *testing.T) {
	stubPlatformStores(t, nil, nil)
	for _, store := range []string{"wincredman", "secretservice", "dpapi", "gpg", "cache", "keychain", "none", "unknown"} {
		env := testEnvironment(t, "", map[string]string{"GCM_CREDENTIAL_STORE": store})
		if _, err := resolveHelper("manager", env); !errors.Is(err, ErrUnsupportedHelper) {
			t.Errorf("store %q: err = %v, want ErrUnsupportedHelper", store, err)
		}
	}
}

func TestResolveHelperManagerPlaintextDefaultsToTheGCMDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	env := testEnvironment(t, "", map[string]string{"GCM_CREDENTIAL_STORE": "plaintext"})
	store := managerStore(t, "manager", env)
	if fs := store.(*gcmFileStore); fs.root != filepath.Join(home, ".gcm", "store") {
		t.Fatalf("root = %q", fs.root)
	}
}

func TestResolveHelperManagerPlaintextFailsWithoutHome(t *testing.T) {
	env := testEnvironment(t, "", map[string]string{"GCM_CREDENTIAL_STORE": "plaintext"})
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	if _, err := resolveHelper("manager", env); err == nil || errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("err = %v, want a home directory error", err)
	}
}

func TestResolveHelperManagerPlaintextFailsOnAnUnexpandablePath(t *testing.T) {
	env := testEnvironment(t, "", map[string]string{"GCM_CREDENTIAL_STORE": "plaintext", "GCM_PLAINTEXT_STORE_PATH": "~badname/store"})
	if _, err := resolveHelper("manager", env); err == nil || errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("err = %v, want an expansion error", err)
	}
}

func TestResolveHelperStoreDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, err := resolveHelper("store", emptyEnvironment(t))
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	sh := h.(*storeHelper)
	want := filepath.Join(home, ".git-credentials")
	if sh.path != want {
		t.Fatalf("path = %q, want %q", sh.path, want)
	}
	if sh.Name() != "store" {
		t.Fatalf("Name() = %q, want %q", sh.Name(), "store")
	}
}

func TestResolveHelperStoreExplicitFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "creds.txt")
	h, err := resolveHelper("store --file="+target, emptyEnvironment(t))
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	sh := h.(*storeHelper)
	if sh.path != target {
		t.Fatalf("path = %q, want %q", sh.path, target)
	}
}

func TestResolveHelperStoreDefaultPathFailsWithoutHome(t *testing.T) {
	env := emptyEnvironment(t)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	if _, err := resolveHelper("store", env); err == nil {
		t.Fatalf("resolveHelper(store) succeeded despite no home directory being available")
	}
}

func TestResolveHelperStoreInvalidFileExpansionFails(t *testing.T) {
	if _, err := resolveHelper("store --file=~badname", emptyEnvironment(t)); err == nil {
		t.Fatalf("resolveHelper succeeded despite an unexpandable ~ prefix")
	}
}

func TestResolveHelperStoreExpandsHomeInFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, err := resolveHelper(`store --file=~/creds.txt`, emptyEnvironment(t))
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	sh := h.(*storeHelper)
	want := filepath.Join(home, "creds.txt")
	if sh.path != want {
		t.Fatalf("path = %q, want %q", sh.path, want)
	}
}
