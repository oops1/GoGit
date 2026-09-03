package systheme

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestSchemeString(t *testing.T) {
	if Dark.String() != "dark" || Light.String() != "light" || Unknown.String() != "unknown" {
		t.Fatal("scheme strings")
	}
}

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func writeConfig(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectFromDesktopFiles(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		files map[string]string
		want  Scheme
	}{
		{name: "nothing", want: Unknown},
		{name: "gtk theme env dark", env: map[string]string{"GTK_THEME": "Adwaita:dark"}, want: Dark},
		{name: "gtk theme env light", env: map[string]string{"GTK_THEME": "Adwaita"}, want: Light},
		{name: "gtk4 prefer dark", files: map[string]string{"gtk-4.0/settings.ini": "[Settings]\ngtk-application-prefer-dark-theme=1\n"}, want: Dark},
		{name: "gtk3 prefer dark true", files: map[string]string{"gtk-3.0/settings.ini": "[Settings]\ngtk-application-prefer-dark-theme = true\n"}, want: Dark},
		{name: "gtk3 prefer light with dark name", files: map[string]string{"gtk-3.0/settings.ini": "[Settings]\ngtk-application-prefer-dark-theme=0\ngtk-theme-name=Yaru-dark\n"}, want: Dark},
		{name: "gtk3 prefer light no name", files: map[string]string{"gtk-3.0/settings.ini": "[Settings]\ngtk-application-prefer-dark-theme=false\n"}, want: Light},
		{name: "gtk3 theme name only", files: map[string]string{"gtk-3.0/settings.ini": "# comment\n; other\n[Other]\nx=1\n[Settings]\ngtk-theme-name=Adwaita\nbroken line\n"}, want: Light},
		{name: "gtk3 settings without keys", files: map[string]string{"gtk-3.0/settings.ini": "[Settings]\n"}, want: Unknown},
		{name: "kde dark", files: map[string]string{"kdeglobals": "[General]\nColorScheme=BreezeDark\n"}, want: Dark},
		{name: "kde light", files: map[string]string{"kdeglobals": "[General]\nColorScheme=BreezeLight\n"}, want: Light},
		{name: "kde without scheme", files: map[string]string{"kdeglobals": "[General]\nName=x\n"}, want: Unknown},
		{name: "gtk4 wins over kde", files: map[string]string{"gtk-4.0/settings.ini": "[Settings]\ngtk-application-prefer-dark-theme=1\n", "kdeglobals": "[General]\nColorScheme=BreezeLight\n"}, want: Dark},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tc.files {
				writeConfig(t, dir, rel, content)
			}
			env := map[string]string{"XDG_CONFIG_HOME": dir}
			for k, v := range tc.env {
				env[k] = v
			}
			if got := detectFromDesktopFiles(envOf(env), "unused"); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetectFromDesktopFilesUsesHome(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, ".config/gtk-3.0/settings.ini", "[Settings]\ngtk-application-prefer-dark-theme=1\n")
	if got := detectFromDesktopFiles(envOf(nil), home); got != Dark {
		t.Fatalf("got %v", got)
	}
}

func TestReadINIMissingFile(t *testing.T) {
	if readINI(filepath.Join(t.TempDir(), "nope"), "x") != nil {
		t.Fatal("missing file must return nil")
	}
}

func TestWatchReportsChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		var current atomic.Int32
		current.Store(int32(Light))
		seen := make(chan Scheme, 8)
		done := make(chan struct{})
		go func() {
			defer close(done)
			watch(ctx, time.Second, func() Scheme { return Scheme(current.Load()) }, func(s Scheme) bool { seen <- s; return true })
		}()
		time.Sleep(1500 * time.Millisecond)
		current.Store(int32(Dark))
		time.Sleep(2 * time.Second)
		cancel()
		<-done
		close(seen)
		var got []Scheme
		for s := range seen {
			got = append(got, s)
		}
		if len(got) != 1 || got[0] != Dark {
			t.Fatalf("seen = %v", got)
		}
	})
}

func TestWatchPublicWrapper(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	Watch(ctx, time.Hour, func(Scheme) bool { t.Fatal("must not fire"); return false })
}
