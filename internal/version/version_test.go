package version

import (
	"runtime/debug"
	"testing"
)

func TestStringUsesLinkerValueOrBuildInfo(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "v1.2.3"
	if String() != "v1.2.3" {
		t.Fatalf("got %q", String())
	}
	Version = "dev"
	if String() == "" {
		t.Fatal("empty version")
	}
}

func TestResolve(t *testing.T) {
	tagged := &debug.BuildInfo{Main: debug.Module{Version: "v0.9.0"}}
	devel := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	cases := []struct {
		name   string
		linked string
		info   *debug.BuildInfo
		ok     bool
		want   string
	}{
		{"linker wins", "v2.0.0", tagged, true, "v2.0.0"},
		{"build info tag", "dev", tagged, true, "v0.9.0"},
		{"devel build", "dev", devel, true, "dev"},
		{"empty version", "dev", &debug.BuildInfo{}, true, "dev"},
		{"no build info", "dev", nil, false, "dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.linked, tc.info, tc.ok); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
