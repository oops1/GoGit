package remote

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
)

func TestRemoteMapsBranchesThroughItsFetchRefspecs(t *testing.T) {
	custom, err := refspec.ParseAll([]string{"+refs/heads/main:refs/remotes/mirror/trunk", "+refs/heads/*:refs/remotes/mirror/all/*"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		rem      Remote
		branch   refs.Name
		tracking refs.Name
	}{
		{"default refspec", Remote{Name: "origin"}, "refs/heads/topic", "refs/remotes/origin/topic"},
		{"exact refspec wins first", Remote{Name: "origin", Fetch: custom}, "refs/heads/main", "refs/remotes/mirror/trunk"},
		{"wildcard refspec", Remote{Name: "origin", Fetch: custom}, "refs/heads/topic", "refs/remotes/mirror/all/topic"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := tc.rem.TrackingRef(tc.branch); !ok || got != tc.tracking {
				t.Fatalf("TrackingRef(%s) = %s, %v, want %s", tc.branch, got, ok, tc.tracking)
			}
			if got, ok := tc.rem.UpstreamOf(tc.tracking); !ok || got != tc.branch {
				t.Fatalf("UpstreamOf(%s) = %s, %v, want %s", tc.tracking, got, ok, tc.branch)
			}
		})
	}
	narrow := Remote{Name: "origin", Fetch: custom[:1]}
	if got, ok := narrow.TrackingRef("refs/heads/topic"); ok {
		t.Fatalf("a branch outside the refspecs mapped to %s", got)
	}
	if got, ok := narrow.UpstreamOf("refs/remotes/origin/topic"); ok {
		t.Fatalf("a ref outside the refspecs mapped back to %s", got)
	}
}
