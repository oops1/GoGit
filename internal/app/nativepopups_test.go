package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"
)

func TestTheNativePopupHostIsRemovedOnceTheWindowInstallsIt(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(func() { widget.SetPopupsHosted(false) })
	var checks atomic.Int32
	hosted := func() bool {
		if checks.Add(1) < 3 {
			return false
		}
		a.eng.SetPopupSink(func([]engine.PopupFrame) {})
		return true
	}

	a.keepPopupsInCanvas(t.Context(), hosted)

	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, widget.PopupsHosted) {
		if time.Now().After(deadline) {
			t.Fatal("the popup host is still installed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWaitingForThePopupHostStopsWithTheWindow(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		a.keepPopupsInCanvas(ctx, func() bool { return false })
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("the wait did not stop after the window closed")
	}
}
