package app

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/ui/credentials"
	"github.com/oops1/gogit/internal/ui/unlock"
	"github.com/oops1/gogit/internal/vault"
)

const vaultIdleTime = 15 * time.Minute

var (
	newCredentialsView = credentials.NewView
	newUnlockView      = unlock.NewView
	openVaultFile      = vault.Open
)

type appCredentialSource struct {
	app *App
}

func (a *App) credentialSource() transport.CredentialSource {
	return appCredentialSource{app: a}
}

func (s appCredentialSource) Credentials(ctx context.Context, resource string, retry bool) (transport.Credentials, error) {
	a := s.app
	username := ""
	if !retry {
		if cred, ok := a.vaultCredential(resource); ok {
			return transport.Credentials{Username: cred.Username, Password: cred.Secret}, nil
		}
	} else if cred, ok := a.vaultCredential(resource); ok {
		username = cred.Username
		cred.Wipe()
	}
	result, ok := a.askCredentials(ctx, resource, username, retry)
	if !ok {
		return transport.Credentials{}, transport.ErrNoCredentials
	}
	if result.Remember {
		a.rememberCredential(ctx, resource, result.Username, result.Secret)
	}
	return transport.Credentials{Username: result.Username, Password: result.Secret}, nil
}

func (a *App) vaultCredential(resource string) (vault.Credential, bool) {
	v := a.vaultIfOpen()
	if v == nil {
		return vault.Credential{}, false
	}
	return v.Credential(resource)
}

func (a *App) vaultIfOpen() *vault.Vault {
	a.vaultMu.Lock()
	defer a.vaultMu.Unlock()
	if a.vaultInst != nil {
		return a.vaultInst
	}
	v, err := openVaultFile(vault.Options{Path: a.paths.VaultFile(), IdleTime: vaultIdleTime})
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			a.log.Warn("open vault failed", "error", err)
		}
		return nil
	}
	a.vaultInst = v
	return v
}

func (a *App) askCredentials(ctx context.Context, resource, username string, retry bool) (credentials.Result, bool) {
	type response struct {
		result credentials.Result
		ok     bool
	}
	ch := make(chan response, 1)
	a.Post(func() {
		view, err := newCredentialsView(a.eng, credentials.Request{Resource: resource, Username: username, Retry: retry})
		if err != nil {
			a.log.Warn("open credentials dialog failed", "error", err)
			ch <- response{}
			return
		}
		view.OnOK = func(res credentials.Result) {
			a.eng.CloseModal(view.Dialog())
			ch <- response{result: res, ok: true}
		}
		view.OnCancel = func() {
			a.eng.CloseModal(view.Dialog())
			ch <- response{}
		}
		a.eng.ShowModal(view.Dialog())
	})
	select {
	case r := <-ch:
		return r.result, r.ok
	case <-ctx.Done():
		return credentials.Result{}, false
	}
}

func (a *App) rememberCredential(ctx context.Context, resource, username string, secret []byte) {
	v := a.vaultIfOpen()
	if v == nil {
		a.log.Warn("cannot remember credential: secret store is not set up", "resource", resource)
		return
	}
	if v.Locked() {
		unlocked, err := a.unlockVault(ctx, v)
		if err != nil {
			a.log.Warn("unlock secret store for credential failed", "resource", resource, "error", err)
			return
		}
		if !unlocked {
			return
		}
	}
	cred := vault.Credential{Resource: resource, Username: username, Secret: append([]byte(nil), secret...)}
	err := v.SetCredential(cred)
	cred.Wipe()
	if err != nil {
		a.log.Warn("save credential failed", "resource", resource, "error", err)
	}
}

func (a *App) unlockVault(ctx context.Context, v *vault.Vault) (bool, error) {
	retry := false
	for {
		password, ok := a.askUnlockPassword(ctx, retry)
		if !ok {
			return false, nil
		}
		err := v.Unlock(ctx, vault.NewPasswordUnlocker(password, vault.SlotParams{}))
		clear(password)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, vault.ErrWrongKey) && !errors.Is(err, vault.ErrSlotNotFound) {
			return false, err
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		retry = true
	}
}

func (a *App) askUnlockPassword(ctx context.Context, retry bool) ([]byte, bool) {
	type response struct {
		password []byte
		ok       bool
	}
	ch := make(chan response, 1)
	a.Post(func() {
		view, err := newUnlockView(a.eng, unlock.Request{Retry: retry})
		if err != nil {
			a.log.Warn("open unlock dialog failed", "error", err)
			ch <- response{}
			return
		}
		view.OnOK = func(res unlock.Result) {
			a.eng.CloseModal(view.Dialog())
			ch <- response{password: res.Password, ok: true}
		}
		view.OnCancel = func() {
			a.eng.CloseModal(view.Dialog())
			ch <- response{}
		}
		a.eng.ShowModal(view.Dialog())
	})
	select {
	case r := <-ch:
		return r.password, r.ok
	case <-ctx.Done():
		return nil, false
	}
}
