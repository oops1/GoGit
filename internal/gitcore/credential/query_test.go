package credential

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestParseQueryHTTPSWithPort(t *testing.T) {
	q, err := ParseQuery("https://alice@example.com:8443/org/repo.git", true)
	if err != nil {
		t.Fatalf("ParseQuery returned %v", err)
	}
	want := Query{Protocol: "https", Host: "example.com:8443", Path: "org/repo.git", Username: "alice"}
	if q != want {
		t.Fatalf("ParseQuery = %+v, want %+v", q, want)
	}
}

func TestParseQueryPathOmittedWithoutUseHTTPPath(t *testing.T) {
	q, err := ParseQuery("https://example.com/org/repo.git", false)
	if err != nil {
		t.Fatalf("ParseQuery returned %v", err)
	}
	if q.Path != "" {
		t.Fatalf("Path = %q, want empty", q.Path)
	}
	if q.Host != "example.com" {
		t.Fatalf("Host = %q, want example.com", q.Host)
	}
}

func TestParseQuerySCPLikeSSHAddress(t *testing.T) {
	q, err := ParseQuery("git@github.com:oops1/gogit.git", true)
	if err != nil {
		t.Fatalf("ParseQuery returned %v", err)
	}
	want := Query{Protocol: "ssh", Host: "github.com", Path: "oops1/gogit.git", Username: "git"}
	if q != want {
		t.Fatalf("ParseQuery = %+v, want %+v", q, want)
	}
}

func TestParseQueryInvalidURLPropagatesError(t *testing.T) {
	_, err := ParseQuery("", true)
	if !errors.Is(err, transport.ErrInvalidURL) {
		t.Fatalf("ParseQuery returned %v, want ErrInvalidURL", err)
	}
}
