package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const atomBody = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <title>v9.9.9</title>
    <link rel="alternate" type="text/html" href="https://example.test/releases/tag/v9.9.9"/>
  </entry>
</feed>`

func serving(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

func TestASpentQuotaIsToldApartFromOtherFailures(t *testing.T) {
	url := serving(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := downloadReleaseFrom(t.Context(), url)

	if !errors.Is(err, ErrReleaseRateLimited) {
		t.Fatalf("error = %v, want the spent quota", err)
	}
	if got := updateFailureText(err); got == "" || got == updateFailureText(ErrReleaseUnavailable) {
		t.Fatalf("message = %q, want its own wording", got)
	}
}

func TestAPlainRefusalKeepsItsStatus(t *testing.T) {
	url := serving(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })

	_, err := downloadReleaseFrom(t.Context(), url)

	if !errors.Is(err, ErrReleaseUnavailable) || errors.Is(err, ErrReleaseRateLimited) {
		t.Fatalf("error = %v, want a plain refusal", err)
	}
}

func TestTheAtomFeedAnswersWhenTheApiWillNot(t *testing.T) {
	feed := serving(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(atomBody))
	})
	restore := releaseFeedURL
	releaseFeedURL = serving(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	})
	t.Cleanup(func() { releaseFeedURL = restore })
	prev := fetchReleaseFeed
	fetchReleaseFeed = func(ctx context.Context, _ string) (releaseInfo, error) {
		return downloadReleaseAtom(ctx, feed)
	}
	t.Cleanup(func() { fetchReleaseFeed = prev })

	info, err := latestRelease(t.Context())

	if err != nil || info.Tag != "v9.9.9" || info.Page != "https://example.test/releases/tag/v9.9.9" {
		t.Fatalf("latestRelease = %+v, %v", info, err)
	}
}

func TestBothSourcesFailingKeepsTheApiError(t *testing.T) {
	restore := releaseFeedURL
	releaseFeedURL = serving(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	t.Cleanup(func() { releaseFeedURL = restore })
	prev := fetchReleaseFeed
	fetchReleaseFeed = func(context.Context, string) (releaseInfo, error) { return releaseInfo{}, errors.New("no feed") }
	t.Cleanup(func() { fetchReleaseFeed = prev })

	if _, err := latestRelease(t.Context()); !errors.Is(err, ErrReleaseUnavailable) {
		t.Fatalf("error = %v, want the api failure", err)
	}
}

func TestAnEmptyOrBrokenAtomFeedIsReported(t *testing.T) {
	empty := serving(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	})
	if _, err := downloadReleaseAtom(t.Context(), empty); !errors.Is(err, ErrReleaseUnavailable) {
		t.Fatalf("empty feed error = %v", err)
	}
	broken := serving(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<feed")) })
	if _, err := downloadReleaseAtom(t.Context(), broken); err == nil {
		t.Fatal("a broken feed was accepted")
	}
	refused := serving(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if _, err := downloadReleaseAtom(t.Context(), refused); !errors.Is(err, ErrReleaseUnavailable) {
		t.Fatalf("refused feed error = %v", err)
	}
}

func TestAFeedRequestThatCannotBeBuiltOrSentIsReported(t *testing.T) {
	if _, err := downloadReleaseAtom(t.Context(), "://broken"); err == nil {
		t.Fatal("a malformed address was accepted")
	}
	if _, err := downloadReleaseAtom(t.Context(), "http://127.0.0.1:1/feed"); err == nil {
		t.Fatal("an unreachable address was accepted")
	}
}
