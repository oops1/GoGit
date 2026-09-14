package app

var drivePosts = func(*App) func() { return func() {} }

func (a *App) startPostQueue() {
	a.stopPostDriver = drivePosts(a)
}

func (a *App) Post(fn func()) {
	if a.postClosed.Load() {
		return
	}
	a.eng.Post(fn)
}

func (a *App) closePostQueue() {
	a.postClosed.Store(true)
	a.stopPostDriver()
}
