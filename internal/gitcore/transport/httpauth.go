package transport

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var nowFunc = time.Now

const headerWWWAuthenticate = "WWW-Authenticate"

const (
	schemeBasic     = "basic"
	schemeBearer    = "bearer"
	schemeNTLM      = "ntlm"
	schemeNegotiate = "negotiate"
	schemeDigest    = "digest"
)

var errAuthHandshakeAborted = errors.New("transport: connection authentication did not complete")

func parseAuthChallenges(h http.Header, key string) map[string]string {
	out := map[string]string{}
	for _, value := range h.Values(key) {
		scheme, token := splitChallenge(value)
		if scheme == "" {
			continue
		}
		if _, seen := out[scheme]; !seen || token != "" {
			out[scheme] = token
		}
	}
	return out
}

func splitChallenge(value string) (scheme, token string) {
	value = strings.TrimSpace(value)
	name, rest, _ := strings.Cut(value, " ")
	name = strings.ToLower(strings.TrimSpace(name))
	rest = strings.TrimSpace(rest)
	switch name {
	case schemeNTLM, schemeNegotiate, schemeBasic, schemeBearer:
		if comma := strings.IndexByte(rest, ','); comma >= 0 {
			rest = strings.TrimSpace(rest[:comma])
		}
		return name, rest
	case schemeDigest:
		return name, rest
	default:
		return "", ""
	}
}

func selectAuthScheme(challenges map[string]string) string {
	for _, scheme := range []string{schemeNegotiate, schemeNTLM, schemeBasic, schemeBearer} {
		if _, ok := challenges[scheme]; ok {
			return scheme
		}
	}
	return ""
}

func onlyUnsupportedChallenges(h http.Header) bool {
	challenges := parseAuthChallenges(h, headerWWWAuthenticate)
	if len(challenges) == 0 {
		return false
	}
	return selectAuthScheme(challenges) == ""
}

func challengeToken(h http.Header, key, scheme string) []byte {
	raw, ok := parseAuthChallenges(h, key)[scheme]
	if !ok || raw == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil
	}
	return decoded
}

func localWorkstation() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	if dot := strings.IndexByte(name, '.'); dot >= 0 {
		name = name[:dot]
	}
	return strings.ToUpper(name)
}

type authGenerator interface {
	next(serverToken []byte) (authValue string, last bool, err error)
	close()
}

type ntlmGenerator struct {
	scheme      string
	creds       ntlmCredentials
	workstation string
	negotiate   []byte
}

func newNTLMGenerator(creds ntlmCredentials) *ntlmGenerator {
	return &ntlmGenerator{scheme: schemeNTLM, creds: creds, workstation: localWorkstation()}
}

func newSPNEGOGenerator(creds ntlmCredentials) *ntlmGenerator {
	return &ntlmGenerator{scheme: schemeNegotiate, creds: creds, workstation: localWorkstation()}
}

func (g *ntlmGenerator) headerScheme() string {
	if g.scheme == schemeNegotiate {
		return "Negotiate"
	}
	return "NTLM"
}

func (g *ntlmGenerator) next(serverToken []byte) (string, bool, error) {
	if g.negotiate == nil {
		g.negotiate = buildNegotiateMessage()
		token := g.negotiate
		if g.scheme == schemeNegotiate {
			token = spnegoNegTokenInit(g.negotiate)
		}
		return g.headerScheme() + " " + base64.StdEncoding.EncodeToString(token), false, nil
	}
	challengeBytes := serverToken
	if g.scheme == schemeNegotiate {
		inner, err := spnegoExtractNTLM(serverToken)
		if err != nil {
			return "", true, err
		}
		challengeBytes = inner
	}
	challenge, err := parseChallengeMessage(challengeBytes)
	if err != nil {
		return "", true, err
	}
	in := authenticateInputs{
		creds:           g.creds,
		challenge:       challenge,
		clientChallenge: randomBytes(8),
		timestamp:       windowsTimestamp(nowFunc()),
		sessionKey:      randomBytes(16),
		workstation:     g.workstation,
	}
	auth := buildAuthenticateMessage(in, g.negotiate, challengeBytes)
	if g.scheme == schemeNegotiate {
		auth = spnegoNegTokenResp(auth)
	}
	return g.headerScheme() + " " + base64.StdEncoding.EncodeToString(auth), true, nil
}

func (g *ntlmGenerator) close() { g.creds.wipe() }

func (c *ntlmCredentials) wipe() {
	for i := range c.password {
		c.password[i] = 0
	}
	c.password = nil
}

func (s *httpSession) newAuthGenerator(ctx context.Context, scheme string) (authGenerator, error) {
	useIntegrated := s.settings.emptyAuth || s.opts.Credentials == nil
	if useIntegrated {
		if gen, ok, err := newIntegratedGenerator(scheme, s.integratedTarget()); ok {
			if err != nil {
				return nil, err
			}
			return gen, nil
		}
	}
	if s.opts.Credentials == nil {
		return nil, ErrNoCredentials
	}
	user, password, err := s.fetchConnectionCredentials(ctx)
	if err != nil {
		return nil, err
	}
	creds := newNTLMCredentials(user, password)
	if scheme == schemeNegotiate {
		return newSPNEGOGenerator(creds), nil
	}
	return newNTLMGenerator(creds), nil
}

func (s *httpSession) integratedTarget() string {
	host := s.endpoint.Host
	if host == "" {
		return ""
	}
	return "HTTP/" + host
}

func (s *httpSession) fetchConnectionCredentials(ctx context.Context) (string, []byte, error) {
	retry := s.credAttempts > 0
	creds, err := s.opts.Credentials.Credentials(ctx, s.resource, retry)
	s.credAttempts++
	if err != nil {
		return "", nil, err
	}
	password := creds.Password
	if len(password) == 0 && len(creds.Token) > 0 {
		password = creds.Token
	}
	s.supplied = &suppliedCredentials{resource: s.resource, creds: creds}
	return creds.Username, append([]byte(nil), password...), nil
}

func drainClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func (s *httpSession) connectionAuth(ctx context.Context, method, suffix, contentType string, body *requestBody, headers map[string]string, scheme string, first *http.Response) (*http.Response, error) {
	drainClose(first)
	gen, err := s.newAuthGenerator(ctx, scheme)
	if err != nil {
		return nil, err
	}
	defer gen.close()
	previous := s.authHeader
	defer func() { s.authHeader = previous }()
	var token []byte
	for {
		value, last, err := gen.next(token)
		if err != nil {
			return nil, err
		}
		s.authHeader = value
		resp, err := s.send(ctx, method, suffix, contentType, body, headers)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized || last {
			return resp, nil
		}
		token = challengeToken(resp.Header, headerWWWAuthenticate, scheme)
		drainClose(resp)
		if token == nil {
			return nil, errAuthHandshakeAborted
		}
	}
}
