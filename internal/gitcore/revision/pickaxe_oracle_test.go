//go:build oracle

package revision

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

func buildPickaxeRepository(t *testing.T) *oracle {
	t.Helper()
	o := newOracle(t)
	o.commit("born", map[string]string{"a.txt": "alpha needle\n", "b.txt": "beta\n"})
	o.commit("same count", map[string]string{"a.txt": "alpha needle!\n"})
	o.commit("doubled", map[string]string{"a.txt": "needle needle\n"})
	o.commit("context only", map[string]string{"b.txt": "beta\nmore\n"})
	o.checkout("-b", "side")
	o.commit("side adds", map[string]string{"side.txt": "side needle\n"})
	o.checkout("main")
	o.commit("main work", map[string]string{"main.txt": "plain\n"})
	o.merge("side", "merge side")
	o.commit("binary", map[string]string{"data.bin": "needle\x00\x01"})
	o.clock += 60
	o.git("mv", "side.txt", "moved.txt")
	o.git("commit", "-q", "-m", "renamed")
	o.clock += 60
	o.git("rm", "-q", "a.txt")
	o.git("commit", "-q", "-m", "removed")
	return o
}

func TestOraclePickaxeMatchesGitLog(t *testing.T) {
	o := buildPickaxeRepository(t)
	ctx := o.open()
	for _, tt := range []struct {
		name  string
		args  []string
		setup func(*Options)
	}{
		{"string", []string{"-S", "needle"}, func(opts *Options) { opts.Pickaxe = "needle" }},
		{"string on a path", []string{"-S", "needle", "--", "a.txt"}, func(opts *Options) {
			opts.Pickaxe = "needle"
			opts.Paths = []string{"a.txt"}
		}},
		{"regular expression", []string{"-G", "need+le|more"}, func(opts *Options) {
			opts.PickaxeRegexp = regexp.MustCompile("need+le|more")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"log", "--format=%H", "HEAD"}, tt.args...)
			if len(tt.args) > 2 && tt.args[2] == "--" {
				args = append([]string{"log", "--format=%H", tt.args[0], tt.args[1], "HEAD"}, tt.args[2:]...)
			}
			want := o.lines(args...)
			opts, err := Ranges([]string{"HEAD"}, ctx)
			if err != nil {
				t.Fatalf("Ranges returned error %v", err)
			}
			tt.setup(&opts)
			var got []string
			for commit, err := range Walk(t.Context(), opts) {
				if err != nil {
					t.Fatalf("Walk returned error %v", err)
				}
				got = append(got, commit.ID.String())
			}
			if !slices.Equal(got, want) {
				t.Errorf("Walk found\n%s\ngit log found\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}
