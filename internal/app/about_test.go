package app

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/oops1/gogit/internal/ui/about"
)

func TestBrowseHelperProcessDoesNothing(t *testing.T) {}

func stubOpenURL(t *testing.T) *string {
	t.Helper()
	var seen string
	prev := openURLCommand
	openURLCommand = func(url string) *exec.Cmd {
		seen = url
		return exec.Command(os.Args[0], "-test.run=TestBrowseHelperProcessDoesNothing")
	}
	t.Cleanup(func() { openURLCommand = prev })
	return &seen
}

func TestAboutCommandOpensTheDialog(t *testing.T) {
	a := newTestApp(t)

	if !a.Dispatch(CmdAbout) {
		t.Fatal("the About command must be handled")
	}
}

func TestAboutDialogButtonsOpenTheProjectAndTheLicense(t *testing.T) {
	a := newTestApp(t)
	seen := stubOpenURL(t)
	var view *about.View
	prev := newAboutView
	newAboutView = func(info about.Info) (*about.View, error) {
		v, err := prev(info)
		view = v
		return v, err
	}
	t.Cleanup(func() { newAboutView = prev })

	a.openAbout()

	view.OnGitHub()
	if *seen != projectURL {
		t.Fatalf("github link = %q, want %q", *seen, projectURL)
	}
	view.OnLicense()
	if *seen != licenseURL {
		t.Fatalf("license link = %q, want %q", *seen, licenseURL)
	}
	view.OnClose()
}

func TestAboutReportsADialogItCannotOpen(t *testing.T) {
	a := newTestApp(t)
	prev := newAboutView
	newAboutView = func(about.Info) (*about.View, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { newAboutView = prev })

	a.openAbout()
}

func TestBrowseRefusesAnythingButHTTPLinks(t *testing.T) {
	stubOpenURL(t)

	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "mailto:a@b.c"} {
		if err := browse(raw); !errors.Is(err, ErrURLNotBrowsable) {
			t.Fatalf("browse(%q) = %v, want ErrURLNotBrowsable", raw, err)
		}
	}
	if err := browse("https://example.com/\nrm -rf"); err == nil {
		t.Fatal("a link carrying a newline must be refused")
	}
	if err := browse("://"); err == nil {
		t.Fatal("a link that cannot be parsed must be reported")
	}
}

func TestBrowseStartsTheSystemHandlerForHTTPLinks(t *testing.T) {
	seen := stubOpenURL(t)

	if err := browse("https://example.com/repo"); err != nil {
		t.Fatalf("browse returned %v", err)
	}
	if *seen != "https://example.com/repo" {
		t.Fatalf("opened %q", *seen)
	}
}

func TestOpenBrowserLogsAFailingHandler(t *testing.T) {
	a := newTestApp(t)
	prev := openURLCommand
	openURLCommand = func(string) *exec.Cmd { return exec.Command("gogit-no-such-binary") }
	t.Cleanup(func() { openURLCommand = prev })

	a.openBrowser("https://example.com")
}
