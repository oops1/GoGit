package app

import (
	"context"
	"errors"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/settings"
)

var ErrKeyPassphraseRequired = errors.New("app: the key is encrypted and needs a passphrase")

var lsRemoteRefs = remote.LsRemote

func (a *App) testSecretsConnection(view *settings.View, section string) {
	if section == "ssh" {
		a.checkKeyFile(view)
		return
	}
	a.checkRemoteConnection(view)
}

func (a *App) checkRemoteConnection(view *settings.View) {
	rawURL := strings.TrimSpace(view.CredentialResource())
	if rawURL == "" {
		a.reportSecretsCheck(view, i18n.T("Dialog.Settings.Secrets.Status.NeedResource"), true)
		return
	}
	a.reportSecretsCheck(view, i18n.T("Dialog.Settings.Secrets.Status.Testing"), false)
	secretsWG.Go(func() {
		refs, err := lsRemoteRefs(context.Background(), rawURL, a.transportOptions(nil))
		if err != nil {
			a.reportSecretsCheck(view, i18n.Tf("Dialog.Settings.Secrets.Status.ConnectionFailed", err.Error()), true)
			return
		}
		a.reportSecretsCheck(view, i18n.Tf("Dialog.Settings.Secrets.Status.ConnectionOk", len(refs)), false)
	})
}

func (a *App) checkKeyFile(view *settings.View) {
	path := strings.TrimSpace(view.KeyPath())
	if path == "" {
		a.reportSecretsCheck(view, i18n.T("Dialog.Settings.Secrets.Status.NeedKeyFile"), true)
		return
	}
	a.reportSecretsCheck(view, i18n.T("Dialog.Settings.Secrets.Status.Testing"), false)
	passphrase := view.TakeKeyPassphrase()
	secretsWG.Go(func() {
		defer clear(passphrase)
		if err := checkPrivateKeyFile(path, passphrase); err != nil {
			a.reportSecretsCheck(view, i18n.Tf("Dialog.Settings.Secrets.Status.KeyFailed", err.Error()), true)
			return
		}
		a.reportSecretsCheck(view, i18n.T("Dialog.Settings.Secrets.Status.KeyOk"), false)
	})
}

func checkPrivateKeyFile(path string, passphrase []byte) error {
	pem, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err = ssh.ParsePrivateKey(pem); err == nil {
		return nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return err
	}
	if len(passphrase) == 0 {
		return ErrKeyPassphraseRequired
	}
	_, err = ssh.ParsePrivateKeyWithPassphrase(pem, passphrase)
	return err
}

func (a *App) reportSecretsCheck(view *settings.View, text string, isError bool) {
	a.Post(func() { view.SetSecretsStatus(text, a.secretsStatusColor(isError)) })
}
