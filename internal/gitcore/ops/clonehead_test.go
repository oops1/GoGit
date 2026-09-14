package ops

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestGuessRemoteHeadPicksTheBranchGitWould(t *testing.T) {
	at, other := hash.ObjectID{1}, hash.ObjectID{2}
	head := transport.Ref{Name: "HEAD", ID: at}
	tag := transport.Ref{Name: "refs/tags/v1", ID: at}
	branch := func(name string, id hash.ObjectID) transport.Ref {
		return transport.Ref{Name: "refs/heads/" + name, ID: id}
	}
	cases := []struct {
		name          string
		advertised    []transport.Ref
		defaultBranch string
		want          string
	}{
		{"no HEAD is advertised", []transport.Ref{branch("main", at)}, "", ""},
		{"HEAD names its branch", []transport.Ref{{Name: "HEAD", ID: at, Symref: "refs/heads/trunk"}, branch("main", at)}, "", "trunk"},
		{"the configured default branch comes first", []transport.Ref{head, branch("alpha", at), branch("main", at), branch("master", at)}, "main", "main"},
		{"master comes before other branches", []transport.Ref{head, branch("alpha", at), branch("main", other), branch("master", at)}, "main", "master"},
		{"otherwise the first branch at HEAD", []transport.Ref{head, tag, branch("alpha", other), branch("beta", at), branch("gamma", at)}, "", "beta"},
		{"no branch is at HEAD", []transport.Ref{head, tag, branch("alpha", other)}, "", ""},
	}
	for _, c := range cases {
		if got := guessRemoteHead(c.advertised, c.defaultBranch); got != c.want {
			t.Errorf("%s: guessRemoteHead = %q, want %q", c.name, got, c.want)
		}
	}
}
