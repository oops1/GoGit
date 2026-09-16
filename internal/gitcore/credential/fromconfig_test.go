package credential

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func loadTestConfig(t *testing.T, content string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	cfg, err := config.Load(config.Options{GitDir: dir, NoSystem: true})
	if err != nil {
		t.Fatalf("config.Load returned %v", err)
	}
	return cfg
}

func helperNames(infos []HelperInfo) []string {
	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name
	}
	return names
}

func TestFromConfigMultiValuedHelperKeepsOrder(t *testing.T) {
	cfg := loadTestConfig(t, "[credential]\n\thelper = store --file=first\n\thelper = store --file=second\n")
	chain, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if !slices.Equal(helperNames(infos), []string{"store --file=first", "store --file=second"}) {
		t.Fatalf("infos = %v, want [first second]", infos)
	}
	if len(chain) != 2 {
		t.Fatalf("len(chain) = %d, want 2", len(chain))
	}
}

func TestFromConfigEmptyValueResetsAccumulatedList(t *testing.T) {
	cfg := loadTestConfig(t, "[credential]\n\thelper = store --file=first\n\thelper =\n\thelper = store --file=second\n")
	chain, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if !slices.Equal(helperNames(infos), []string{"store --file=second"}) {
		t.Fatalf("infos = %v, want [second]", infos)
	}
	if len(chain) != 1 {
		t.Fatalf("len(chain) = %d, want 1", len(chain))
	}
}

func TestFromConfigURLScopedHelperOnlyAppliesWhenHostMatches(t *testing.T) {
	content := "[credential]\n\thelper = store --file=generic\n[credential \"https://example.com\"]\n\thelper = store --file=scoped\n"
	cfg := loadTestConfig(t, content)

	_, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if !slices.Equal(helperNames(infos), []string{"store --file=generic", "store --file=scoped"}) {
		t.Fatalf("infos = %v, want [generic scoped]", infos)
	}

	_, infos, err = FromConfig(cfg, "https://other.example/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if !slices.Equal(helperNames(infos), []string{"store --file=generic"}) {
		t.Fatalf("infos = %v, want [generic]", infos)
	}
}

func TestFromConfigURLScopedHelperSkipsOnProtocolMismatch(t *testing.T) {
	content := "[credential \"http://example.com\"]\n\thelper = store --file=scoped\n"
	cfg := loadTestConfig(t, content)
	_, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("infos = %v, want empty (protocol mismatch)", infos)
	}
}

func TestFromConfigURLScopedHelperSkipsOnUsernameMismatch(t *testing.T) {
	content := "[credential \"https://alice@example.com\"]\n\thelper = store --file=scoped\n"
	cfg := loadTestConfig(t, content)
	_, infos, err := FromConfig(cfg, "https://bob@example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("infos = %v, want empty (username mismatch)", infos)
	}
}

func TestFromConfigResolveErrorPropagates(t *testing.T) {
	cfg := loadTestConfig(t, "[credential]\n\thelper = store --file=~badname\n")
	_, _, err := FromConfig(cfg, "https://example.com/repo.git")
	if err == nil {
		t.Fatalf("FromConfig succeeded despite an unresolvable store helper")
	}
}

func TestFromConfigUseHTTPPathControlsPathScopedHelpers(t *testing.T) {
	content := "[credential]\n\thelper = store --file=generic\n\tuseHttpPath = true\n" +
		"[credential \"https://example.com/org/repo.git\"]\n\thelper = store --file=scoped\n"
	cfg := loadTestConfig(t, content)
	_, infos, err := FromConfig(cfg, "https://example.com/org/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if !slices.Equal(helperNames(infos), []string{"store --file=generic", "store --file=scoped"}) {
		t.Fatalf("infos = %v, want [generic scoped]", infos)
	}

	contentNoPath := "[credential]\n\thelper = store --file=generic\n" +
		"[credential \"https://example.com/org/repo.git\"]\n\thelper = store --file=scoped\n"
	cfgNoPath := loadTestConfig(t, contentNoPath)
	_, infos, err = FromConfig(cfgNoPath, "https://example.com/org/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if !slices.Equal(helperNames(infos), []string{"store --file=generic"}) {
		t.Fatalf("infos = %v, want [generic] when useHttpPath is off", infos)
	}
}

func TestFromConfigDefaultUsernameFeedsURLScopedMatch(t *testing.T) {
	content := "[credential]\n\tusername = alice\n[credential \"https://alice@example.com\"]\n\thelper = store --file=scoped\n"
	cfg := loadTestConfig(t, content)
	_, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if !slices.Equal(helperNames(infos), []string{"store --file=scoped"}) {
		t.Fatalf("infos = %v, want [scoped]", infos)
	}
}

func TestFromConfigMarksUnsupportedHelpersButOmitsThemFromChain(t *testing.T) {
	content := "[credential]\n\thelper = cache\n\thelper = !true\n\thelper = store --file=good\n"
	cfg := loadTestConfig(t, content)
	chain, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	want := []HelperInfo{
		{Name: "cache", Supported: false},
		{Name: "!true", Supported: false},
		{Name: "store --file=good", Supported: true},
	}
	if !slices.Equal(infos, want) {
		t.Fatalf("infos = %+v, want %+v", infos, want)
	}
	if len(chain) != 1 {
		t.Fatalf("len(chain) = %d, want 1", len(chain))
	}
}

func TestFromConfigInvalidURLReturnsError(t *testing.T) {
	cfg := loadTestConfig(t, "[credential]\n\thelper = store --file=good\n")
	_, _, err := FromConfig(cfg, "")
	if err == nil {
		t.Fatalf("FromConfig succeeded despite an empty url")
	}
}

func TestFromConfigInvalidUseHTTPPathValueReturnsError(t *testing.T) {
	cfg := loadTestConfig(t, "[credential]\n\tuseHttpPath = not-a-bool\n")
	_, _, err := FromConfig(cfg, "https://example.com/repo.git")
	if err == nil {
		t.Fatalf("FromConfig succeeded despite an invalid boolean")
	}
}

func TestFromConfigNoHelpersReturnsEmptyChain(t *testing.T) {
	cfg := loadTestConfig(t, "[core]\n\tbare = false\n")
	chain, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if len(chain) != 0 || len(infos) != 0 {
		t.Fatalf("chain = %v, infos = %v, want both empty", chain, infos)
	}
}

func TestFromConfigReportsAManagerWithoutAUsableStoreAsUnsupported(t *testing.T) {
	stubPlatformStores(t, nil, nil)
	t.Setenv("GCM_CREDENTIAL_STORE", "")
	cfg := loadTestConfig(t, "[credential]\n\thelper = manager\n\tcredentialStore = gpg\n")
	chain, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if len(chain) != 0 || !slices.Equal(infos, []HelperInfo{{Name: "manager", Supported: false}}) {
		t.Fatalf("chain = %v, infos = %+v", chain, infos)
	}
}

func TestFromConfigReadsManagerSettingsFromTheEnvironmentFirst(t *testing.T) {
	stubPlatformStores(t, nil, nil)
	t.Setenv("GCM_CREDENTIAL_STORE", "plaintext")
	t.Setenv("GCM_PLAINTEXT_STORE_PATH", t.TempDir())
	cfg := loadTestConfig(t, "[credential]\n\thelper = manager\n\tcredentialStore = gpg\n")
	chain, infos, err := FromConfig(cfg, "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("FromConfig returned %v", err)
	}
	if len(chain) != 1 || !slices.Equal(infos, []HelperInfo{{Name: "manager", Supported: true}}) {
		t.Fatalf("chain = %v, infos = %+v", chain, infos)
	}
}
