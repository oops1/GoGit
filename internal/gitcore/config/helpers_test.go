package config

import (
	"os"
	"path/filepath"
	"testing"
)

func mustParse(t *testing.T, text string) *File {
	t.Helper()
	f, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse(%q) returned error %v", text, err)
	}
	return f
}

func dump(f *File) []string {
	var out []string
	for v := range f.Variables() {
		if v.HasValue {
			out = append(out, v.Name()+"="+v.Value)
		} else {
			out = append(out, v.Name())
		}
	}
	return out
}

func dumpConfig(c *Config) []string {
	var out []string
	for e := range c.All() {
		if e.HasValue {
			out = append(out, e.Name()+"="+e.Value)
		} else {
			out = append(out, e.Name())
		}
	}
	return out
}

func isolateEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HOME", "USERPROFILE", "XDG_CONFIG_HOME",
		"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM",
		"GIT_CONFIG_COUNT", "ProgramData",
	} {
		t.Setenv(key, "")
	}
}

func writeFile(t *testing.T, path, text string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) returned error %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", path, err)
	}
	return path
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error %v", name, err)
	}
	return string(data)
}
