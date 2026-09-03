package app

const postQueueCapacity = 64

func (a *App) startPostQueue() {
	a.postCh = make(chan func(), postQueueCapacity)
	a.postStop = make(chan struct{})
	a.postWG.Go(a.runPostQueue)
}

func (a *App) runPostQueue() {
	for {
		select {
		case fn := <-a.postCh:
			fn()
		case <-a.postStop:
			return
		}
	}
}

func (a *App) Post(fn func()) {
	select {
	case a.postCh <- fn:
	case <-a.postStop:
	}
}
