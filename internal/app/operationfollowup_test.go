package app

import (
	"context"
	"testing"
	"time"
)

func TestAFollowUpStartsAnotherOperationOnlyAfterTheFirstEnds(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	second := make(chan struct{})
	busyWhenFollowedUp := make(chan bool, 1)

	runOnDispatcher(t, a, func() {
		a.RunOperation("first", func(_ context.Context, reporter OperationReporter) error {
			reporter.Then(func() {
				busyWhenFollowedUp <- a.busy()
				a.RunOperation("second", func(context.Context, OperationReporter) error {
					close(second)
					return nil
				})
			})
			return nil
		})
	})

	select {
	case busy := <-busyWhenFollowedUp:
		if busy {
			t.Fatal("the follow-up ran while the first operation still held the busy guard")
		}
	case <-time.After(testTimeout):
		t.Fatal("the follow-up never ran")
	}
	select {
	case <-second:
	case <-time.After(testTimeout):
		t.Fatal("the operation started by the follow-up was refused")
	}
	if count := readOnDispatcher(t, a, func() int { return len(*views) }); count != 2 {
		t.Fatalf("operation windows = %d, want 2", count)
	}
}
