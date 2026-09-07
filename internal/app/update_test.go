package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oops1/gogit/internal/i18n"
)

func stubLatestRelease(t *testing.T, info releaseInfo, err error) {
	t.Helper()
	prev := fetchRelease
	fetchRelease = func(context.Context, string) (releaseInfo, error) { return info, err }
	t.Cleanup(func() { fetchRelease = prev })
}

type recordedMessage struct {
	title   string
	message string
}

func captureMessages(t *testing.T, a *App) (*recordedMessage, *recordedMessage, *func(bool)) {
	t.Helper()
	var info, failure recordedMessage
	var answer func(bool)
	a.showInfo = func(title, message string) { info = recordedMessage{title, message} }
	a.showError = func(title, message string) { failure = recordedMessage{title, message} }
	a.askConfirm = func(_, message string, cb func(bool)) {
		info = recordedMessage{i18n.T("Dialog.Update.Title"), message}
		answer = cb
	}
	return &info, &failure, &answer
}

func TestCheckForUpdatesSaysTheBuildIsCurrent(t *testing.T) {
	a := newTestApp(t)
	info, failure, _ := captureMessages(t, a)

	a.reportUpdate("v1.1.0", releaseInfo{Tag: "v1.1.0"}, nil)

	if failure.message != "" {
		t.Fatalf("unexpected error message %q", failure.message)
	}
	if info.title != i18n.T("Dialog.Update.Title") {
		t.Fatalf("title = %q", info.title)
	}
	if info.message != i18n.Tf("Dialog.Update.UpToDate", "v1.1.0") {
		t.Fatalf("message = %q, want the up-to-date answer", info.message)
	}
}

func TestCheckForUpdatesRunsTheLookupOffTheUIGoroutine(t *testing.T) {
	a := newTestApp(t)
	info, _, _ := captureMessages(t, a)
	stubOpenURL(t)
	stubLatestRelease(t, releaseInfo{Tag: "v99.0.0"}, nil)

	a.checkForUpdates()
	updateWG.Wait()
	waitForPostQueueDrain(t, a)

	if info.message == "" {
		t.Fatal("the answer must reach the window")
	}
}

func TestCheckForUpdatesOffersTheDownloadPage(t *testing.T) {
	a := newTestApp(t)
	info, _, answer := captureMessages(t, a)
	seen := stubOpenURL(t)
	stubLatestRelease(t, releaseInfo{Tag: "v99.0.0", Page: "https://example.com/releases/v99"}, nil)

	a.checkForUpdates()
	updateWG.Wait()
	waitForPostQueueDrain(t, a)

	if info.message == "" {
		t.Fatal("a newer release must be announced")
	}
	(*answer)(false)
	if *seen != "" {
		t.Fatalf("declining must open nothing, opened %q", *seen)
	}
	(*answer)(true)
	if *seen != "https://example.com/releases/v99" {
		t.Fatalf("opened %q, want the release page", *seen)
	}
}

func TestCheckForUpdatesFallsBackToTheReleasesPage(t *testing.T) {
	a := newTestApp(t)
	_, _, answer := captureMessages(t, a)
	seen := stubOpenURL(t)
	stubLatestRelease(t, releaseInfo{Tag: "v99.0.0"}, nil)

	a.checkForUpdates()
	updateWG.Wait()
	waitForPostQueueDrain(t, a)
	(*answer)(true)

	if *seen != latestReleasePageURL {
		t.Fatalf("opened %q, want %q", *seen, latestReleasePageURL)
	}
}

func TestCheckForUpdatesReportsAFailedLookup(t *testing.T) {
	a := newTestApp(t)
	_, failure, _ := captureMessages(t, a)
	stubLatestRelease(t, releaseInfo{}, errors.New("no network"))

	a.checkForUpdates()
	updateWG.Wait()
	waitForPostQueueDrain(t, a)

	if failure.title != i18n.T("Dialog.Update.Title") {
		t.Fatalf("title = %q", failure.title)
	}
	if failure.message != i18n.Tf("Dialog.Update.Failed", "no network") {
		t.Fatalf("message = %q", failure.message)
	}
}

func TestReleaseComparisonUnderstandsSemanticVersions(t *testing.T) {
	newer := [][2]string{
		{"v1.1.0", "v1.0.9"},
		{"v1.2", "v1.1.9"},
		{"v2.0.0", "v1.99.99"},
		{"v1.0.1", "v1.0.0-rc1"},
	}
	for _, pair := range newer {
		if !isNewerRelease(pair[1], pair[0]) {
			t.Fatalf("%q must count as newer than %q", pair[0], pair[1])
		}
	}
	sameOrOlder := [][2]string{
		{"v1.0.0", "v1.0.0"},
		{"v1.0.0", "v1.0.1"},
		{"1.0.0", "v1.0.0"},
	}
	for _, pair := range sameOrOlder {
		if isNewerRelease(pair[1], pair[0]) {
			t.Fatalf("%q must not count as newer than %q", pair[0], pair[1])
		}
	}
	if isNewerRelease("v1.0.0", "") {
		t.Fatal("a feed without a tag offers no update")
	}
	if !isNewerRelease("dev", "v1.0.0") {
		t.Fatal("a development build must be offered the latest release")
	}
}

func TestDownloadLatestReleaseReadsTheTagAndPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","html_url":"https://example.com/v1.2.3"}`))
	}))
	t.Cleanup(srv.Close)

	info, err := downloadReleaseFrom(t.Context(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if info.Tag != "v1.2.3" || info.Page != "https://example.com/v1.2.3" {
		t.Fatalf("info = %+v", info)
	}
}

func TestDownloadLatestReleaseReportsAnErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	if _, err := downloadReleaseFrom(t.Context(), srv.URL); !errors.Is(err, ErrReleaseUnavailable) {
		t.Fatalf("err = %v, want ErrReleaseUnavailable", err)
	}
}

func TestDownloadLatestReleaseReportsUnreadableAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)

	if _, err := downloadReleaseFrom(t.Context(), srv.URL); err == nil {
		t.Fatal("an answer that is not JSON must be reported")
	}
}

func TestDownloadLatestReleaseReportsAnUnreachableHost(t *testing.T) {
	if _, err := downloadReleaseFrom(t.Context(), "http://127.0.0.1:1/releases"); err == nil {
		t.Fatal("an unreachable host must be reported")
	}
	if _, err := downloadReleaseFrom(t.Context(), "://"); err == nil {
		t.Fatal("an address that cannot be parsed must be reported")
	}
}

func TestUpdateCommandIsHandled(t *testing.T) {
	a := newTestApp(t)
	captureMessages(t, a)
	stubOpenURL(t)
	stubLatestRelease(t, releaseInfo{Tag: "v0.0.1"}, nil)

	if !a.Dispatch(CmdCheckUpdates) {
		t.Fatal("the update command must be handled")
	}
	updateWG.Wait()
	waitForPostQueueDrain(t, a)
}
