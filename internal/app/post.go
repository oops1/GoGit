package app

func (a *App) startPostQueue() {
	a.postWake = make(chan struct{}, 1)
	a.postStop = make(chan struct{})
	a.postWG.Go(a.runPostQueue)
}

func (a *App) runPostQueue() {
	for {
		select {
		case <-a.postWake:
			for _, fn := range a.takePosted() {
				fn()
			}
		case <-a.postStop:
			return
		}
	}
}

func (a *App) takePosted() []func() {
	a.postMu.Lock()
	defer a.postMu.Unlock()
	posted := a.posted
	a.posted = nil
	return posted
}

func (a *App) Post(fn func()) {
	a.postMu.Lock()
	if a.postClosed {
		a.postMu.Unlock()
		return
	}
	a.posted = append(a.posted, fn)
	a.postMu.Unlock()
	select {
	case a.postWake <- struct{}{}:
	default:
	}
}

func (a *App) closePostQueue() {
	a.postMu.Lock()
	a.postClosed = true
	a.posted = nil
	a.postMu.Unlock()
	close(a.postStop)
	a.postWG.Wait()
}
