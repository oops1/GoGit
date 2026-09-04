package app

import (
	"testing"
	"time"

	"github.com/oops1/gogit/internal/config"
)

func TestPostDoesNotBlockWhileTheQueueGoroutineIsBusy(t *testing.T) {
	a := newTestApp(t)
	release := make(chan struct{})
	a.Post(func() { <-release })

	posted := make(chan struct{})
	go func() {
		for range 4 * postFloodSize {
			a.Post(func() {})
		}
		close(posted)
	}()

	select {
	case <-posted:
	case <-time.After(testTimeout):
		close(release)
		t.Fatal("Post blocked while the queue goroutine was busy")
	}
	close(release)
	waitForPostQueueDrain(t, a)
}

const postFloodSize = 64

func TestPostRunsEveryCallbackInOrder(t *testing.T) {
	a := newTestApp(t)
	order := make(chan int, postFloodSize)
	for i := range postFloodSize {
		a.Post(func() { order <- i })
	}
	waitForPostQueueDrain(t, a)
	for i := range postFloodSize {
		select {
		case got := <-order:
			if got != i {
				t.Fatalf("callback %d ran at position %d", got, i)
			}
		default:
			t.Fatalf("callback %d never ran", i)
		}
	}
}

func TestPostAfterCloseIsDropped(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default())
	a.Close()

	ran := make(chan struct{})
	a.Post(func() { close(ran) })

	select {
	case <-ran:
		t.Fatal("a callback posted after Close must not run")
	case <-time.After(50 * time.Millisecond):
	}
	if posted := a.takePosted(); len(posted) != 0 {
		t.Fatalf("queue kept %d callbacks after Close", len(posted))
	}
}
