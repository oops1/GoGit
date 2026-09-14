package app

import (
	"testing"
	"time"
)

var productionDrivePosts = drivePosts

func init() {
	drivePosts = driveTestPosts
}

func driveTestPosts(a *App) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				a.eng.Flush()
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func TestTheRunningEngineDrainsPostsOnItsOwn(t *testing.T) {
	stop := productionDrivePosts(nil)

	stop()
}

func TestPostsReachTheEngineQueue(t *testing.T) {
	a := newTestApp(t)
	a.stopPostDriver()
	a.stopPostDriver = func() {}
	ran := false

	a.Post(func() { ran = true })
	a.Engine().Flush()

	if !ran {
		t.Fatal("a posted callback did not run when the engine queue was flushed")
	}
}
