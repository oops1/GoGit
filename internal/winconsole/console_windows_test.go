package winconsole

import "testing"

func stubConsole(t *testing.T, window uintptr, clients int, released bool) *int {
	t.Helper()
	prevWindow, prevClients, prevRelease := consoleWindow, consoleClients, releaseConsole
	calls := 0
	consoleWindow = func() uintptr { return window }
	consoleClients = func() int { return clients }
	releaseConsole = func() bool { calls++; return released }
	t.Cleanup(func() {
		consoleWindow, consoleClients, releaseConsole = prevWindow, prevClients, prevRelease
	})
	return &calls
}

func TestHideLeavesAConsoleSharedWithAnotherProcess(t *testing.T) {
	calls := stubConsole(t, 0x1234, 2, true)
	if Hide() {
		t.Fatal("a console shared with a shell must stay open")
	}
	if *calls != 0 {
		t.Fatalf("FreeConsole calls = %d, want none", *calls)
	}
}

func TestHideDoesNothingWithoutAConsole(t *testing.T) {
	calls := stubConsole(t, 0, 1, true)
	if Hide() {
		t.Fatal("a windowsgui build has no console to hide")
	}
	if *calls != 0 {
		t.Fatalf("FreeConsole calls = %d, want none", *calls)
	}
}

func TestHideReleasesAConsoleOwnedByTheProcess(t *testing.T) {
	calls := stubConsole(t, 0x1234, 1, true)
	if !Hide() {
		t.Fatal("a console owned by this process must be released")
	}
	if *calls != 1 {
		t.Fatalf("FreeConsole calls = %d, want one", *calls)
	}
}

func TestHideReportsAFailedRelease(t *testing.T) {
	stubConsole(t, 0x1234, 1, false)
	if Hide() {
		t.Fatal("a failed FreeConsole must be reported")
	}
}

func TestOwnConsoleHelpersRunAgainstTheSystem(t *testing.T) {
	clients := ownConsoleClients()
	if ownConsoleWindow() != 0 && clients < 1 {
		t.Fatal("a real console must list at least this process")
	}
	if clients < 0 {
		t.Fatalf("console clients = %d", clients)
	}
}
