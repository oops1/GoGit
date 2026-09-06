package app

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/ui/hostkey"
)

var newHostKeyView = hostkey.NewView

func (a *App) hostKeyPolicy() transport.HostKeyPolicy {
	return transport.NewKnownHosts(a.paths.KnownHosts(), a.confirmHostKey)
}

func (a *App) confirmHostKey(ctx context.Context, key transport.HostKey) (bool, error) {
	type response struct{ accept bool }
	ch := make(chan response, 1)
	a.Post(func() {
		view, err := newHostKeyView(a.eng, hostkey.Request{
			Host:        key.Host,
			Algorithm:   key.Algorithm,
			Fingerprint: key.Fingerprint,
		})
		if err != nil {
			a.log.Warn("open host key dialog failed", "error", err)
			ch <- response{}
			return
		}
		view.OnAccept = func() {
			a.eng.CloseModal(view.Dialog())
			ch <- response{accept: true}
		}
		view.OnReject = func() {
			a.eng.CloseModal(view.Dialog())
			ch <- response{}
		}
		a.eng.ShowModal(view.Dialog())
	})
	select {
	case r := <-ch:
		return r.accept, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
