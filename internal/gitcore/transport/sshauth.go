package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var (
	ErrNoKeys  = errors.New("transport: no usable ssh keys")
	ErrNoAgent = errors.New("transport: ssh agent is not available")

	errKeyLocked = errors.New("transport: ssh key needs a passphrase")
)

const maxPassphraseAttempts = 3

var agentDialer = dialAgent

type SSHTarget struct {
	Host           string
	HostName       string
	Port           string
	User           string
	IdentityFiles  []string
	IdentitiesOnly bool
}

type SSHKeySource interface {
	SSHKeys(ctx context.Context, target SSHTarget) ([]Key, error)
}

type PassphraseRequest struct {
	Host  string
	Path  string
	Retry bool
}

type PassphraseSource interface {
	Passphrase(ctx context.Context, req PassphraseRequest) ([]byte, error)
}

type PassphraseFeedback interface {
	ApprovePassphrase(ctx context.Context, req PassphraseRequest, passphrase []byte)
}

type SSHOptions struct {
	ConfigFiles []string
	Overrides   []string
	Passphrases PassphraseSource
}

type signerLister interface {
	listSigners(ctx context.Context, target SSHTarget, held *closerSet) ([]ssh.Signer, error)
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

type closerSet struct {
	mu    sync.Mutex
	items []io.Closer
}

func (c *closerSet) add(item io.Closer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append(c.items, item)
}

func (c *closerSet) closeAll() {
	c.mu.Lock()
	items := c.items
	c.items = nil
	c.mu.Unlock()
	for i := len(items) - 1; i >= 0; i-- {
		closeQuietly(items[i])
	}
}

type dirKeySource struct {
	dir string
}

var dirKeyFileNames = []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_dsa"}

func NewDirKeys(dir string) KeySource {
	return dirKeySource{dir: dir}
}

func (d dirKeySource) Keys(ctx context.Context, host string) ([]Key, error) {
	return d.SSHKeys(ctx, SSHTarget{Host: host})
}

func (d dirKeySource) SSHKeys(ctx context.Context, target SSHTarget) ([]Key, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths := target.IdentityFiles
	if len(paths) == 0 {
		for _, name := range dirKeyFileNames {
			paths = append(paths, filepath.Join(d.dir, name))
		}
	}
	var keys []Key
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		keys = append(keys, Key{Path: path, Private: data})
	}
	return keys, nil
}

type multiKeySource struct {
	sources []KeySource
}

func MultiKeys(sources ...KeySource) KeySource {
	return &multiKeySource{sources: sources}
}

func (m *multiKeySource) Keys(ctx context.Context, host string) ([]Key, error) {
	var all []Key
	var lastErr error
	for _, src := range m.sources {
		if src == nil {
			continue
		}
		keys, err := src.Keys(ctx, host)
		if err != nil {
			lastErr = err
			continue
		}
		all = append(all, keys...)
	}
	if len(all) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return all, nil
}

type agentKeySource struct{}

func NewAgentKeys() KeySource {
	return agentKeySource{}
}

func (agentKeySource) Keys(ctx context.Context, host string) ([]Key, error) {
	held := &closerSet{}
	defer held.closeAll()
	signers, err := agentSigners(ctx, SSHTarget{Host: host}, held)
	if err != nil {
		return nil, err
	}
	keys := make([]Key, 0, len(signers))
	for _, s := range signers {
		keys = append(keys, Key{Path: "agent:" + ssh.FingerprintSHA256(s.PublicKey())})
	}
	return keys, nil
}

func (agentKeySource) listSigners(ctx context.Context, target SSHTarget, held *closerSet) ([]ssh.Signer, error) {
	return agentSigners(ctx, target, held)
}

func agentSigners(ctx context.Context, target SSHTarget, held *closerSet) ([]ssh.Signer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := agentDialer()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoKeys, err)
	}
	stop := watchContextClose(ctx, conn)
	release := closerFunc(func() error {
		stop()
		return conn.Close()
	})
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		closeQuietly(release)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%w: %w", ErrNoKeys, err)
	}
	signers = identitySigners(signers, target)
	if len(signers) == 0 {
		closeQuietly(release)
		return nil, ErrNoKeys
	}
	held.add(release)
	return signers, nil
}

func identitySigners(signers []ssh.Signer, target SSHTarget) []ssh.Signer {
	if !target.IdentitiesOnly {
		return signers
	}
	var allowed [][]byte
	for _, path := range target.IdentityFiles {
		if pub := identityPublicKey(path); pub != nil {
			allowed = append(allowed, pub.Marshal())
		}
	}
	var kept []ssh.Signer
	for _, signer := range signers {
		blob := signer.PublicKey().Marshal()
		for _, want := range allowed {
			if bytes.Equal(blob, want) {
				kept = append(kept, signer)
				break
			}
		}
	}
	return kept
}

func identityPublicKey(path string) ssh.PublicKey {
	if data, err := os.ReadFile(path + ".pub"); err == nil {
		if pub, _, _, _, err := ssh.ParseAuthorizedKey(data); err == nil {
			return pub
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil
	}
	return signer.PublicKey()
}

func watchContextClose(ctx context.Context, conn io.Closer) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			closeQuietly(conn)
		case <-done:
		}
	}()
	return func() { close(done) }
}

func keysFor(ctx context.Context, source KeySource, target SSHTarget) ([]Key, error) {
	if targeted, ok := source.(SSHKeySource); ok {
		return targeted.SSHKeys(ctx, target)
	}
	return source.Keys(ctx, target.Host)
}

type lockedKey struct {
	key   Key
	retry bool
}

type sshAuth struct {
	ctx     context.Context
	target  SSHTarget
	prompt  PassphraseSource
	held    closerSet
	ready   []ssh.Signer
	locked  []lockedKey
	lastErr error
	offered bool
}

func newSSHAuth(ctx context.Context, target SSHTarget, source KeySource, prompt PassphraseSource) (*sshAuth, error) {
	a := &sshAuth{ctx: ctx, target: target, prompt: prompt}
	if source == nil {
		return nil, fmt.Errorf("%w: no key source is configured", ErrNoKeys)
	}
	a.collect(source)
	if err := ctx.Err(); err != nil {
		a.release()
		return nil, err
	}
	if len(a.ready) == 0 && len(a.locked) == 0 {
		a.release()
		if a.lastErr != nil {
			return nil, a.lastErr
		}
		return nil, ErrNoKeys
	}
	return a, nil
}

func (a *sshAuth) collect(source KeySource) {
	switch src := source.(type) {
	case *multiKeySource:
		for _, sub := range src.sources {
			if sub != nil {
				a.collect(sub)
			}
		}
	case signerLister:
		signers, err := src.listSigners(a.ctx, a.target, &a.held)
		if err != nil {
			a.lastErr = err
			return
		}
		a.ready = append(a.ready, signers...)
	default:
		keys, err := keysFor(a.ctx, source, a.target)
		if err != nil {
			a.lastErr = err
			return
		}
		a.addKeys(keys)
	}
}

func (a *sshAuth) addKeys(keys []Key) {
	for _, k := range keys {
		signer, err := signerFromKey(k)
		switch {
		case err == nil:
			a.ready = append(a.ready, signer)
		case a.prompt != nil && errors.Is(err, errKeyLocked):
			a.locked = append(a.locked, lockedKey{key: Key{Path: k.Path, Private: k.Private}, retry: len(k.Passphrase) > 0})
		default:
			a.lastErr = err
		}
		clear(k.Passphrase)
	}
}

func (a *sshAuth) method() ssh.AuthMethod {
	return ssh.RetryableAuthMethod(ssh.PublicKeysCallback(a.next), 0)
}

func (a *sshAuth) next() ([]ssh.Signer, error) {
	if !a.offered {
		a.offered = true
		if len(a.ready) > 0 {
			return a.ready, nil
		}
	}
	for len(a.locked) > 0 {
		pending := a.locked[0]
		a.locked = a.locked[1:]
		signer, err := a.unlock(pending)
		if err != nil {
			return nil, err
		}
		if signer != nil {
			return []ssh.Signer{signer}, nil
		}
	}
	return nil, fmt.Errorf("%w: the server accepted none of the offered ssh keys", ErrAccessDenied)
}

func (a *sshAuth) unlock(pending lockedKey) (ssh.Signer, error) {
	req := PassphraseRequest{Host: a.target.Host, Path: pending.key.Path, Retry: pending.retry}
	for range maxPassphraseAttempts {
		passphrase, err := a.prompt.Passphrase(a.ctx, req)
		if err != nil {
			return nil, a.ctx.Err()
		}
		signer, err := ssh.ParsePrivateKeyWithPassphrase(pending.key.Private, passphrase)
		if err == nil {
			if feedback, ok := a.prompt.(PassphraseFeedback); ok {
				feedback.ApprovePassphrase(a.ctx, req, passphrase)
			}
			clear(passphrase)
			return signer, nil
		}
		clear(passphrase)
		req.Retry = true
	}
	return nil, nil
}

func (a *sshAuth) release() {
	a.held.closeAll()
}

func signerFromKey(k Key) (ssh.Signer, error) {
	if len(k.Private) == 0 {
		return nil, fmt.Errorf("%w: key %q has no private key material", ErrNoKeys, k.Path)
	}
	signer, err := ssh.ParsePrivateKey(k.Private)
	if err == nil {
		return signer, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, fmt.Errorf("transport: parse ssh key %q: %w", k.Path, err)
	}
	if len(k.Passphrase) == 0 {
		return nil, fmt.Errorf("%w: ssh key %q is encrypted and no passphrase was supplied", errKeyLocked, k.Path)
	}
	signer, err = ssh.ParsePrivateKeyWithPassphrase(k.Private, k.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt ssh key %q: %w", errKeyLocked, k.Path, err)
	}
	return signer, nil
}
