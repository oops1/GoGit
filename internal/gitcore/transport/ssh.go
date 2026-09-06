package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var (
	ErrNoKeys  = errors.New("transport: no usable ssh keys")
	ErrNoAgent = errors.New("transport: ssh agent is not available")
)

const defaultSSHPort = "22"

var agentDialer = dialAgent

var dialSSHTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, addr)
}

type signerLister interface {
	listSigners(ctx context.Context, host string) ([]ssh.Signer, error)
}

type dirKeySource struct {
	dir string
}

var dirKeyFileNames = []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_dsa"}

func NewDirKeys(dir string) KeySource {
	return dirKeySource{dir: dir}
}

func (d dirKeySource) Keys(ctx context.Context, _ string) ([]Key, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var keys []Key
	for _, name := range dirKeyFileNames {
		path := filepath.Join(d.dir, name)
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

func (m *multiKeySource) listSigners(ctx context.Context, host string) ([]ssh.Signer, error) {
	var all []ssh.Signer
	var lastErr error
	for _, src := range m.sources {
		if src == nil {
			continue
		}
		signers, err := gatherSigners(ctx, host, src)
		if err != nil {
			lastErr = err
			continue
		}
		all = append(all, signers...)
	}
	if len(all) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrNoKeys
	}
	return all, nil
}

type agentKeySource struct{}

func NewAgentKeys() KeySource {
	return agentKeySource{}
}

func (agentKeySource) Keys(ctx context.Context, _ string) ([]Key, error) {
	signers, err := agentSigners(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]Key, 0, len(signers))
	for _, s := range signers {
		keys = append(keys, Key{Path: "agent:" + ssh.FingerprintSHA256(s.PublicKey())})
	}
	return keys, nil
}

func (agentKeySource) listSigners(ctx context.Context, _ string) ([]ssh.Signer, error) {
	return agentSigners(ctx)
}

func agentSigners(ctx context.Context) ([]ssh.Signer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := agentDialer()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoKeys, err)
	}
	defer closeQuietly(conn)
	stop := watchContextClose(ctx, conn)
	defer stop()
	client := agent.NewClient(conn)
	signers, err := client.Signers()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%w: %w", ErrNoKeys, err)
	}
	if len(signers) == 0 {
		return nil, ErrNoKeys
	}
	return signers, nil
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

func gatherSigners(ctx context.Context, host string, source KeySource) ([]ssh.Signer, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: no key source is configured", ErrNoKeys)
	}
	if sl, ok := source.(signerLister); ok {
		return sl.listSigners(ctx, host)
	}
	keys, err := source.Keys(ctx, host)
	if err != nil {
		return nil, err
	}
	return signersFromKeys(keys)
}

func signersFromKeys(keys []Key) ([]ssh.Signer, error) {
	if len(keys) == 0 {
		return nil, ErrNoKeys
	}
	var signers []ssh.Signer
	var lastErr error
	for _, k := range keys {
		signer, err := signerFromKey(k)
		if err != nil {
			lastErr = err
			continue
		}
		signers = append(signers, signer)
	}
	if len(signers) == 0 {
		return nil, lastErr
	}
	return signers, nil
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
		return nil, fmt.Errorf("transport: ssh key %q is encrypted and no passphrase was supplied", k.Path)
	}
	signer, err = ssh.ParsePrivateKeyWithPassphrase(k.Private, k.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("transport: decrypt ssh key %q: %w", k.Path, err)
	}
	return signer, nil
}

var currentUser = user.Current

func sshUsername(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if u, err := currentUser(); err == nil && u.Username != "" {
		return u.Username
	}
	return ""
}

func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func sshExecCommand(service Service, path string) string {
	return string(service) + " " + shellQuoteSingle(path)
}

func hostKeyCallback(ctx context.Context, policy HostKeyPolicy) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		if policy == nil {
			return fmt.Errorf("%w: no host key policy is configured", ErrHostKeyRejected)
		}
		return policy.Check(ctx, HostKey{
			Host:        hostname,
			Algorithm:   key.Type(),
			Fingerprint: ssh.FingerprintSHA256(key),
			Key:         key.Marshal(),
		})
	}
}

type sshSession struct {
	mu         sync.Mutex
	endpoint   Endpoint
	service    Service
	opts       Options
	rawConn    net.Conn
	client     *ssh.Client
	sess       *ssh.Session
	stdin      io.WriteCloser
	stdout     io.Reader
	advertised bool
	version    int
	caps       Capabilities
	closed     bool
}

func newSSHSession(endpoint Endpoint, service Service, opts Options) *sshSession {
	return &sshSession{endpoint: endpoint, service: service, opts: opts}
}

func (s *sshSession) connect(ctx context.Context) error {
	if s.rawConn != nil {
		return nil
	}
	signers, err := gatherSigners(ctx, s.endpoint.Host, s.opts.Keys)
	if err != nil {
		return err
	}
	port := s.endpoint.Port
	if port == "" {
		port = defaultSSHPort
	}
	addr := net.JoinHostPort(s.endpoint.Host, port)
	conn, err := dialSSHTCP(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	stop := watchContext(ctx, conn)
	defer stop()
	client, sess, stdin, stdout, err := s.handshakeAndExec(ctx, conn, addr, signers)
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	s.rawConn = conn
	s.client = client
	s.sess = sess
	s.stdin = stdin
	s.stdout = stdout
	return nil
}

func (s *sshSession) handshakeAndExec(ctx context.Context, conn net.Conn, addr string, signers []ssh.Signer) (*ssh.Client, *ssh.Session, io.WriteCloser, io.Reader, error) {
	config := &ssh.ClientConfig{
		User:            sshUsername(s.endpoint.User),
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signers...)},
		HostKeyCallback: hostKeyCallback(ctx, s.opts.HostKeys),
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	sess, err := client.NewSession()
	if err != nil {
		closeQuietly(client)
		return nil, nil, nil, nil, err
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	if s.opts.Version != 1 {
		_ = sess.Setenv("GIT_PROTOCOL", "version=2")
	}
	if err := sess.Start(sshExecCommand(s.service, s.endpoint.Path)); err != nil {
		closeQuietly(client)
		return nil, nil, nil, nil, err
	}
	return client, sess, stdin, stdout, nil
}

func (s *sshSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.sess != nil {
		closeQuietly(s.sess)
	}
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type sshRoundTripper struct {
	session *sshSession
}

func (rt sshRoundTripper) round(ctx context.Context, body []byte) (io.ReadCloser, error) {
	s := rt.session
	if _, err := s.stdin.Write(body); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return noCloseReader{Reader: s.stdout}, nil
}

func (s *sshSession) advertiseLocked(ctx context.Context) (Advertisement, error) {
	adv, err := ParseAdvertisement(s.stdout)
	if err != nil {
		return Advertisement{}, err
	}
	s.version = adv.Version
	if s.version == 0 {
		s.version = 1
	}
	s.caps = adv.Capabilities
	if s.version == 2 {
		refs, head, err := lsRefsV2(ctx, sshRoundTripper{session: s}, agentValue(s.opts))
		if err != nil {
			return Advertisement{}, err
		}
		adv.Refs = refs
		adv.Head = head
	}
	adv.Version = s.version
	s.advertised = true
	return adv, nil
}

func (s *sshSession) ensureAdvertised(ctx context.Context) error {
	if s.advertised {
		return nil
	}
	_, err := s.advertiseLocked(ctx)
	return err
}

func (s *sshSession) Advertise(ctx context.Context) (Advertisement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Advertisement{}, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	if err := s.connect(ctx); err != nil {
		return Advertisement{}, err
	}
	stop := watchContext(ctx, s.rawConn)
	defer stop()
	adv, err := s.advertiseLocked(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return Advertisement{}, ctx.Err()
		}
		return Advertisement{}, err
	}
	return adv, nil
}

func (s *sshSession) Fetch(ctx context.Context, req FetchRequest, neg Negotiator) (*FetchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	if err := s.connect(ctx); err != nil {
		return nil, err
	}
	stop := watchContext(ctx, s.rawConn)
	release := true
	defer func() {
		if release {
			stop()
		}
	}()
	if err := s.ensureAdvertised(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	rt := sshRoundTripper{session: s}
	agentValueText := agentValue(s.opts)
	var resp *FetchResponse
	var err error
	if s.version == 2 {
		resp, err = fetchV2(ctx, rt, req, neg, s.caps, agentValueText)
	} else {
		resp, err = fetchV1(ctx, rt, req, neg, s.caps, agentValueText, false)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if resp.Pack != nil {
		release = false
		pack := resp.Pack
		resp.Pack = readCloser{Reader: pack, closer: func() error {
			stop()
			return pack.Close()
		}}
	}
	return resp, nil
}

func (s *sshSession) Push(ctx context.Context, req PushRequest) (*PushResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	if err := s.connect(ctx); err != nil {
		return nil, err
	}
	stop := watchContext(ctx, s.rawConn)
	defer stop()
	result, err := s.pushLocked(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return result, nil
}

func (s *sshSession) pushLocked(ctx context.Context, req PushRequest) (*PushResult, error) {
	if err := s.ensureAdvertised(ctx); err != nil {
		return nil, err
	}
	prefix, _ := buildPushRequest(req, s.caps, agentValue(s.opts))
	if _, err := s.stdin.Write(prefix); err != nil {
		return nil, err
	}
	if _, err := io.Copy(s.stdin, req.Pack); err != nil {
		return nil, err
	}
	return finishPush(s.stdout, s.caps)
}
