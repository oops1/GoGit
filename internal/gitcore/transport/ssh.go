package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/user"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

const defaultSSHPort = "22"

var dialSSHTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, addr)
}

var currentUser = user.Current

func sshUsername(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if u, err := currentUser(); err == nil && u.Username != "" {
		return u.Username[strings.LastIndexByte(u.Username, '\\')+1:]
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

func hostKeyPolicyFor(policy HostKeyPolicy, hop sshHop) HostKeyPolicy {
	known, ok := policy.(*knownHostsPolicy)
	if !ok {
		return policy
	}
	return known.forHop(hop.knownHostsFiles, hop.strictHostKeys)
}

type sshSession struct {
	mu         sync.Mutex
	endpoint   Endpoint
	service    Service
	opts       Options
	rawConn    net.Conn
	jumps      []*ssh.Client
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

type sshDialState struct {
	ctx     context.Context
	rawConn net.Conn
	jumps   []*ssh.Client
	stop    func()
}

func (d *sshDialState) fail(err error) error {
	for i := len(d.jumps) - 1; i >= 0; i-- {
		closeQuietly(d.jumps[i])
	}
	if d.rawConn != nil {
		closeQuietly(d.rawConn)
	}
	if ctxErr := d.ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func (d *sshDialState) dial(hop sshHop) (net.Conn, error) {
	if len(d.jumps) == 0 {
		conn, err := dialSSHTCP(d.ctx, "tcp", hop.addr)
		if err != nil {
			return nil, err
		}
		d.rawConn = conn
		d.stop = watchContext(d.ctx, conn)
		return conn, nil
	}
	return d.jumps[len(d.jumps)-1].DialContext(d.ctx, "tcp", hop.addr)
}

func (s *sshSession) connect(ctx context.Context) error {
	if s.rawConn != nil {
		return nil
	}
	hops, err := resolveSSHRoute(s.endpoint, s.opts.SSH)
	if err != nil {
		return err
	}
	state := &sshDialState{ctx: ctx, stop: func() {}}
	defer func() { state.stop() }()
	for i, hop := range hops {
		client, err := s.connectHop(state, hop)
		if err != nil {
			return state.fail(err)
		}
		if i < len(hops)-1 {
			state.jumps = append(state.jumps, client)
			continue
		}
		sess, stdin, stdout, err := s.startCommand(client)
		if err != nil {
			closeQuietly(client)
			return state.fail(err)
		}
		s.rawConn = state.rawConn
		s.jumps = state.jumps
		s.client = client
		s.sess = sess
		s.stdin = stdin
		s.stdout = stdout
	}
	return nil
}

func (s *sshSession) connectHop(state *sshDialState, hop sshHop) (*ssh.Client, error) {
	auth, err := newSSHAuth(state.ctx, hop.target, s.opts.Keys, s.opts.SSH.Passphrases)
	if err != nil {
		return nil, err
	}
	defer auth.release()
	conn, err := state.dial(hop)
	if err != nil {
		return nil, err
	}
	policy := hostKeyPolicyFor(s.opts.HostKeys, hop)
	config := &ssh.ClientConfig{
		User:            hop.target.User,
		Auth:            []ssh.AuthMethod{auth.method()},
		HostKeyCallback: hostKeyCallback(state.ctx, policy),
	}
	if lister, ok := policy.(interface{ HostKeyAlgorithms(string) []string }); ok {
		config.HostKeyAlgorithms = lister.HostKeyAlgorithms(hop.hostKeyName)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, hop.hostKeyName, config)
	if err != nil {
		closeQuietly(conn)
		return nil, err
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func (s *sshSession) startCommand(client *ssh.Client) (*ssh.Session, io.WriteCloser, io.Reader, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, nil, nil, err
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	if s.opts.Version != 1 {
		_ = sess.Setenv("GIT_PROTOCOL", "version=2")
	}
	if err := sess.Start(sshExecCommand(s.service, s.endpoint.Path)); err != nil {
		return nil, nil, nil, err
	}
	return sess, stdin, stdout, nil
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
	var err error
	if s.client != nil {
		err = s.client.Close()
	}
	for i := len(s.jumps) - 1; i >= 0; i-- {
		closeQuietly(s.jumps[i])
	}
	return err
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
