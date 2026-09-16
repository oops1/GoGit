package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"sync"
	"time"

	"github.com/oops1/gogit/internal/config"
	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/credential"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/credentials"
	"github.com/oops1/gogit/internal/ui/unlock"
	"github.com/oops1/gogit/internal/vault"
)

const vaultIdleTime = 15 * time.Minute

var (
	newCredentialsView   = credentials.NewView
	newUnlockView        = unlock.NewView
	openVaultFile        = vault.Open
	buildCredentialChain = defaultCredentialChainAndQuery
)

type credentialOrigin int

const (
	credentialFromDialog credentialOrigin = iota
	credentialFromVault
	credentialFromHelper
)

type suppliedCredential struct {
	origin       credentialOrigin
	vaultEntry   string
	legacyEntry  bool
	username     string
	secret       []byte
	saveToVault  bool
	saveToHelper bool
	chain        credential.Chain
	query        credential.Query
}

func (c *suppliedCredential) wipe() {
	clear(c.secret)
	c.secret = nil
}

type appCredentialSource struct {
	app           *App
	mu            sync.Mutex
	supplied      map[string]*suppliedCredential
	rejectedUsers map[string]string
}

func (a *App) credentialSource() transport.CredentialSource {
	return &appCredentialSource{
		app:           a,
		supplied:      map[string]*suppliedCredential{},
		rejectedUsers: map[string]string{},
	}
}

func (s *appCredentialSource) Credentials(ctx context.Context, resource string, retry bool) (transport.Credentials, error) {
	a := s.app
	mode := a.cfg.Git.CredentialSource

	if !retry && mode != config.CredentialSourceHelper {
		if cred, legacy, ok := a.vaultCredentialFor(resource); ok {
			s.record(resource, &suppliedCredential{
				origin:      credentialFromVault,
				vaultEntry:  cred.Resource,
				legacyEntry: legacy,
				username:    cred.Username,
				secret:      cred.Secret,
			})
			creds := transport.Credentials{Username: cred.Username, Password: append([]byte(nil), cred.Secret...)}
			cred.Wipe()
			return creds, nil
		}
	}

	var chain credential.Chain
	var q credential.Query
	if mode == config.CredentialSourceVaultThenHelper || mode == config.CredentialSourceHelper {
		chain, q = buildCredentialChain(a, resource)
		if !retry {
			ans, ok, err := chain.Get(ctx, q)
			if err != nil {
				a.log.Warn("query credential helpers failed", "resource", resource, "error", err)
			}
			if ok {
				s.record(resource, &suppliedCredential{
					origin:       credentialFromHelper,
					username:     ans.Username,
					secret:       ans.Password,
					saveToHelper: true,
					chain:        chain,
					query:        q,
				})
				creds := transport.Credentials{Username: ans.Username, Password: append([]byte(nil), ans.Password...)}
				ans.Wipe()
				return creds, nil
			}
		}
	}

	result, ok := a.askCredentials(ctx, resource, s.promptUsername(resource, retry), retry, rememberTargetLabel(mode))
	if !ok {
		return transport.Credentials{}, transport.ErrNoCredentials
	}
	s.record(resource, &suppliedCredential{
		origin:       credentialFromDialog,
		username:     result.Username,
		secret:       result.Secret,
		saveToVault:  result.Remember && mode != config.CredentialSourceHelper,
		saveToHelper: result.Remember && mode == config.CredentialSourceHelper,
		chain:        chain,
		query:        q,
	})
	return transport.Credentials{Username: result.Username, Password: result.Secret}, nil
}

func (s *appCredentialSource) Approve(ctx context.Context, resource string, creds transport.Credentials) {
	supplied := s.take(resource, creds)
	if supplied == nil {
		return
	}
	defer supplied.wipe()
	a := s.app
	if supplied.legacyEntry {
		a.migrateLegacyCredential(supplied.vaultEntry, supplied.username, supplied.secret)
	}
	if supplied.saveToHelper {
		a.storeCredentialToHelper(ctx, supplied.chain, supplied.query, supplied.username, supplied.secret)
	}
	if supplied.saveToVault {
		a.rememberCredential(ctx, resource, supplied.username, supplied.secret)
	}
}

func (s *appCredentialSource) Reject(ctx context.Context, resource string, creds transport.Credentials) {
	supplied := s.take(resource, creds)
	if supplied == nil {
		return
	}
	defer supplied.wipe()
	s.mu.Lock()
	s.rejectedUsers[resource] = supplied.username
	s.mu.Unlock()
	a := s.app
	switch supplied.origin {
	case credentialFromVault:
		a.forgetVaultCredential(supplied.vaultEntry, supplied.secret)
	case credentialFromHelper:
		ans := credential.Answer{Username: supplied.username, Password: supplied.secret}
		if err := supplied.chain.Erase(ctx, supplied.query, ans); err != nil {
			a.log.Warn("erase rejected credential from helper failed", "resource", resource, "error", err)
		}
	}
}

func (s *appCredentialSource) record(resource string, supplied *suppliedCredential) {
	supplied.secret = append([]byte(nil), supplied.secret...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous := s.supplied[resource]; previous != nil {
		previous.wipe()
	}
	s.supplied[resource] = supplied
}

func (s *appCredentialSource) take(resource string, creds transport.Credentials) *suppliedCredential {
	s.mu.Lock()
	defer s.mu.Unlock()
	supplied := s.supplied[resource]
	if supplied == nil || supplied.username != creds.Username || subtle.ConstantTimeCompare(supplied.secret, creds.Password) != 1 {
		return nil
	}
	delete(s.supplied, resource)
	return supplied
}

func (s *appCredentialSource) promptUsername(resource string, retry bool) string {
	if endpoint, _, _ := transport.ParseURL(resource); endpoint.User != "" {
		return endpoint.User
	}
	if !retry {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rejectedUsers[resource]
}

func rememberTargetLabel(mode string) string {
	if mode == config.CredentialSourceHelper {
		return i18n.T("Dialog.Credentials.Target.Helper")
	}
	return i18n.T("Dialog.Credentials.Target.Vault")
}

func defaultCredentialChainAndQuery(a *App, resource string) (credential.Chain, credential.Query) {
	cfg := a.repositoryConfigForCredentials()
	q := credentialQueryFor(cfg, resource)
	chain, _, err := credential.FromConfig(cfg, resource)
	if err != nil {
		a.log.Warn("resolve credential helpers failed", "resource", resource, "error", err)
		return nil, q
	}
	return chain, q
}

func credentialQueryFor(cfg *gitconfig.Config, rawURL string) credential.Query {
	q, err := credential.QueryFromConfig(cfg, rawURL)
	if err != nil {
		return credential.Query{}
	}
	return q
}

func (a *App) repositoryConfigForCredentials() *gitconfig.Config {
	o := a.opened()
	if o == nil {
		return &gitconfig.Config{}
	}
	r, err := a.freshRepo(o)
	if err != nil {
		return &gitconfig.Config{}
	}
	defer func() { _ = r.Close() }()
	return r.Config()
}

func (a *App) storeCredentialToHelper(ctx context.Context, chain credential.Chain, q credential.Query, username string, secret []byte) {
	ans := credential.Answer{Username: username, Password: append([]byte(nil), secret...)}
	if err := chain.Store(ctx, q, ans); err != nil {
		a.log.Warn("save credential to helper failed", "error", err)
	}
	ans.Wipe()
}

func credentialResourceKey(typed string) string {
	endpoint, password, err := transport.ParseURL(typed)
	password.Wipe()
	if err != nil || (endpoint.Scheme != transport.SchemeHTTP && endpoint.Scheme != transport.SchemeHTTPS) {
		return typed
	}
	return transport.CredentialResource(endpoint)
}

type vaultLookup struct {
	key    string
	user   string
	legacy bool
}

func vaultLookupsFor(resource string) []vaultLookup {
	lookups := []vaultLookup{{key: resource}}
	endpoint, _, _ := transport.ParseURL(resource)
	if endpoint.User != "" {
		anonymous := endpoint
		anonymous.User = ""
		lookups = append(lookups, vaultLookup{key: transport.CredentialResource(anonymous), user: endpoint.User})
	}
	if endpoint.Scheme == transport.SchemeHTTPS && endpoint.Port == "" && endpoint.User == "" {
		lookups = append(lookups, vaultLookup{key: endpoint.Host + endpoint.Path, legacy: true})
	}
	return lookups
}

func (a *App) vaultCredentialFor(resource string) (vault.Credential, bool, bool) {
	v := a.vaultIfOpen()
	if v == nil {
		return vault.Credential{}, false, false
	}
	for _, lookup := range vaultLookupsFor(resource) {
		cred, ok := v.Credential(lookup.key)
		if ok && (lookup.user == "" || cred.Username == lookup.user) {
			return cred, lookup.legacy, true
		}
		cred.Wipe()
	}
	return vault.Credential{}, false, false
}

func (a *App) migrateLegacyCredential(entry, username string, secret []byte) {
	v := a.vaultIfOpen()
	if v == nil {
		return
	}
	migrated := vault.Credential{Resource: "https://" + entry, Username: username, Secret: append([]byte(nil), secret...)}
	err := v.SetCredential(migrated)
	migrated.Wipe()
	if err == nil {
		err = v.DeleteCredential(entry)
	}
	if err != nil {
		a.log.Warn("move saved credential to the url key failed", "resource", entry, "error", err)
	}
}

func (a *App) forgetVaultCredential(entry string, secret []byte) {
	v := a.vaultIfOpen()
	if v == nil {
		return
	}
	current, ok := v.Credential(entry)
	stale := ok && current.Resource == entry && subtle.ConstantTimeCompare(current.Secret, secret) == 1
	current.Wipe()
	if !stale {
		return
	}
	if err := v.DeleteCredential(entry); err != nil {
		a.log.Warn("remove rejected credential failed", "resource", entry, "error", err)
	}
}

func (a *App) vaultIfOpen() *vault.Vault {
	v, _ := a.openVault()
	return v
}

func (a *App) askCredentials(ctx context.Context, resource, username string, retry bool, rememberTarget string) (credentials.Result, bool) {
	type response struct {
		result credentials.Result
		ok     bool
	}
	ch := make(chan response, 1)
	a.Post(func() {
		view, err := newCredentialsView(a.eng, credentials.Request{
			Resource:       resource,
			Username:       username,
			Retry:          retry,
			RememberTarget: rememberTarget,
		})
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
		a.showModal(view.Dialog(), view)
		view.SetErrorColor(secretsErrorTextColor)
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
	silentErr := a.trySilentUnlock(ctx, v)
	if silentErr == nil {
		return true, nil
	}
	if !v.HasSlot(vault.SlotPassword) {
		return false, errors.Join(ErrSecretsKeyUnavailable, silentErr)
	}
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
		a.showModal(view.Dialog(), view)
	})
	select {
	case r := <-ch:
		return r.password, r.ok
	case <-ctx.Done():
		return nil, false
	}
}
