package app

import (
	"context"
	"time"
)

const nativePopupPoll = 10 * time.Millisecond

func (a *App) keepPopupsInCanvas(ctx context.Context, hosted func() bool) {
	ticker := time.NewTicker(nativePopupPoll)
	defer ticker.Stop()
	for {
		if hosted() {
			a.Post(func() { a.eng.SetPopupSink(nil) })
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
