package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/keypassphrase"
	"github.com/oops1/gogit/internal/vault"
)

var (
	newKeyPassphraseView = keypassphrase.NewView
	sshUserHomeDir       = os.UserHomeDir
)

func (a *App) sshKeySource() transport.KeySource {
	sources := []transport.KeySource{vaultSSHKeys{app: a}, transport.NewAgentKeys()}
	if home, err := sshUserHomeDir(); err == nil && home != "" {
		sources = append(sources, transport.NewDirKeys(filepath.Join(home, ".ssh")))
	}
	return transport.MultiKeys(sources...)
}

type vaultSSHKeys struct {
	app *App
}

func (k vaultSSHKeys) Keys(ctx context.Context, host string) ([]transport.Key, error) {
	return k.SSHKeys(ctx, transport.SSHTarget{Host: host})
}

func (k vaultSSHKeys) SSHKeys(_ context.Context, target transport.SSHTarget) ([]transport.Key, error) {
	v := k.app.vaultIfOpen()
	if v == nil {
		return nil, nil
	}
	entry, ok := v.SSHKey(target.Host)
	if !ok && target.HostName != "" {
		entry, ok = v.SSHKey(target.HostName)
	}
	if !ok {
		return nil, nil
	}
	private := entry.Private
	if len(private) == 0 {
		data, err := os.ReadFile(expandKeyPath(entry.Path))
		if err != nil {
			entry.Wipe()
			return nil, err
		}
		private = data
	}
	return []transport.Key{{Path: entry.Path, Private: private, Passphrase: entry.Passphrase}}, nil
}

func expandKeyPath(path string) string {
	rest, ok := strings.CutPrefix(path, "~")
	if !ok || (rest != "" && !os.IsPathSeparator(rest[0])) {
		return path
	}
	home, err := sshUserHomeDir()
	if err != nil || home == "" {
		return path
	}
	return filepath.Join(home, rest)
}

type keyPassphraseSource struct {
	app      *App
	mu       sync.Mutex
	remember map[string]bool
}

func (a *App) keyPassphraseSource() *keyPassphraseSource {
	return &keyPassphraseSource{app: a, remember: map[string]bool{}}
}

func (s *keyPassphraseSource) Passphrase(ctx context.Context, req transport.PassphraseRequest) ([]byte, error) {
	result, ok := s.app.askKeyPassphrase(ctx, req)
	if !ok {
		return nil, transport.ErrNoCredentials
	}
	s.mu.Lock()
	s.remember[req.Path] = result.Remember
	s.mu.Unlock()
	return result.Passphrase, nil
}

func (s *keyPassphraseSource) ApprovePassphrase(ctx context.Context, req transport.PassphraseRequest, passphrase []byte) {
	s.mu.Lock()
	remember := s.remember[req.Path]
	delete(s.remember, req.Path)
	s.mu.Unlock()
	if remember {
		s.app.rememberKeyPassphrase(ctx, req, passphrase)
	}
}

func (a *App) askKeyPassphrase(ctx context.Context, req transport.PassphraseRequest) (keypassphrase.Result, bool) {
	type response struct {
		result keypassphrase.Result
		ok     bool
	}
	ch := make(chan response, 1)
	a.Post(func() {
		view, err := newKeyPassphraseView(a.eng, keypassphrase.Request{Host: req.Host, Path: req.Path, Retry: req.Retry})
		if err != nil {
			a.log.Warn("open key passphrase dialog failed", "error", err)
			ch <- response{}
			return
		}
		view.OnOK = func(res keypassphrase.Result) {
			a.eng.CloseModal(view.Dialog())
			ch <- response{result: res, ok: true}
		}
		view.OnCancel = func() {
			a.eng.CloseModal(view.Dialog())
			ch <- response{}
		}
		a.showModal(view.Dialog(), view)
		view.SetErrorColor(secretsErrorTextColor)
	})
	select {
	case r := <-ch:
		return r.result, r.ok
	case <-ctx.Done():
		return keypassphrase.Result{}, false
	}
}

var errHostHasAnotherKey = errors.New("app: the host already has another ssh key in the store")

func (a *App) rememberKeyPassphrase(ctx context.Context, req transport.PassphraseRequest, passphrase []byte) {
	v := a.vaultIfOpen()
	if v == nil {
		a.log.Warn("cannot remember key passphrase: secret store is not set up", "host", req.Host)
		return
	}
	if v.Locked() {
		unlocked, err := a.unlockVault(ctx, v)
		if err != nil {
			a.log.Warn("unlock secret store for key passphrase failed", "host", req.Host, "error", err)
			return
		}
		if !unlocked {
			return
		}
	}
	if err := storeKeyPassphrase(v, req, passphrase); err != nil {
		a.log.Warn("save key passphrase failed", "host", req.Host, "key", req.Path, "error", err)
	}
}

func storeKeyPassphrase(v *vault.Vault, req transport.PassphraseRequest, passphrase []byte) error {
	key := vault.SSHKey{Host: req.Host, Path: req.Path, Passphrase: append([]byte(nil), passphrase...)}
	defer key.Wipe()
	if existing, ok := v.SSHKey(req.Host); ok && existing.Host == req.Host {
		sameKey := existing.Path == req.Path
		key.Private = append([]byte(nil), existing.Private...)
		existing.Wipe()
		if !sameKey {
			return errHostHasAnotherKey
		}
	}
	return v.SetSSHKey(key)
}

func (a *App) sshTransportOptions() transport.SSHOptions {
	opts := transport.SSHOptions{ConfigFiles: transport.DefaultSSHConfigFiles(), Passphrases: a.keyPassphraseSource()}
	command, ignored := a.sshCommand()
	if len(ignored) > 0 {
		a.log.Warn("ssh command arguments are not supported and were ignored", "arguments", strings.Join(ignored, " "))
	}
	if command.ConfigFile != "" {
		opts.ConfigFiles = []string{expandKeyPath(command.ConfigFile)}
	}
	opts.Overrides = command.Overrides
	return opts
}

func (a *App) sshCommand() (transport.SSHCommand, []string) {
	raw := os.Getenv("GIT_SSH_COMMAND")
	if raw == "" {
		raw, _ = a.repositoryConfigForCredentials().Get("core.sshcommand")
	}
	command, err := transport.ParseSSHCommand(raw)
	if err != nil {
		return transport.SSHCommand{}, []string{err.Error()}
	}
	return command, command.Ignored
}

func (a *App) reportIgnoredSSHArguments(reporter OperationReporter) {
	if _, ignored := a.sshCommand(); len(ignored) > 0 {
		reporter.Log(i18n.Tf("Operation.Log.SSHCommandIgnored", strings.Join(ignored, " ")))
	}
}

func reportTransportError(reporter OperationReporter, err error) {
	if errors.Is(err, transport.ErrProxyCommandUnsupported) {
		reporter.Log(i18n.T("Operation.Log.ProxyCommandUnsupported"))
	}
}

func transportErrorText(err error) string {
	if errors.Is(err, transport.ErrProxyCommandUnsupported) {
		return i18n.T("Operation.Log.ProxyCommandUnsupported")
	}
	return err.Error()
}
