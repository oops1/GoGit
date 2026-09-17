package transport

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var nowFunc = time.Now

const (
	headerWWWAuthenticate   = "WWW-Authenticate"
	headerProxyAuthenticate = "Proxy-Authenticate"
)

const (
	schemeBasic     = "basic"
	schemeBearer    = "bearer"
	schemeNTLM      = "ntlm"
	schemeNegotiate = "negotiate"
	schemeDigest    = "digest"
)

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
	name, _, _ = strings.Cut(name, ".")
	return strings.ToUpper(name)
}

func basicAuthorization(user *url.Userinfo) string {
	password, _ := user.Password()
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user.Username()+":"+password))
}

type authGenerator interface {
	next(serverToken []byte) (authValue string, last bool, err error)
	close()
}

type ntlmGenerator struct {
	scheme      string
	creds       ntlmCredentials
	workstation string
	binding     []byte
	negotiate   []byte
}

func newCredentialGenerator(scheme string, creds ntlmCredentials, binding []byte) *ntlmGenerator {
	return &ntlmGenerator{scheme: scheme, creds: creds, workstation: localWorkstation(), binding: binding}
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
		channelBinding:  ntlmChannelBindingHash(g.binding),
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
	clear(c.password)
	c.password = nil
}

func drainClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func runHandshake(gen authGenerator, challengeHeader, scheme string, challengeStatus int, leg func(string) (*http.Response, error)) (*http.Response, error) {
	var token []byte
	for {
		value, last, err := gen.next(token)
		if err != nil {
			return nil, err
		}
		resp, err := leg(value)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != challengeStatus || last {
			return resp, nil
		}
		token = challengeToken(resp.Header, challengeHeader, scheme)
		if token == nil {
			return resp, nil
		}
		drainClose(resp)
	}
}

func requestHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (s *httpSession) serverGenerator(ctx context.Context, scheme string, binding []byte, allowIntegrated bool) (authGenerator, bool, error) {
	if allowIntegrated {
		if gen, ok := newIntegratedGenerator(scheme, requestHost(s.baseURL), binding); ok {
			s.integratedAttempted = true
			return gen, true, nil
		}
	}
	if s.opts.Credentials == nil {
		return nil, false, ErrNoCredentials
	}
	user, password, err := s.fetchConnectionCredentials(ctx)
	if err != nil {
		return nil, false, err
	}
	return newCredentialGenerator(scheme, newNTLMCredentials(user, password), binding), false, nil
}

func (s *httpSession) fetchConnectionCredentials(ctx context.Context) (string, []byte, error) {
	retry := s.credAttempts > 0
	creds, err := s.opts.Credentials.Credentials(ctx, s.resource, retry)
	s.credAttempts++
	if err != nil {
		return "", nil, err
	}
	s.supplied = &suppliedCredentials{resource: s.resource, creds: creds}
	return creds.Username, secretOf(creds), nil
}

func secretOf(creds Credentials) []byte {
	if len(creds.Password) == 0 && len(creds.Token) > 0 {
		return append([]byte(nil), creds.Token...)
	}
	return append([]byte(nil), creds.Password...)
}

func (s *httpSession) connectionAuthLoop(ctx context.Context, first *http.Response, method, suffix, contentType string, body *requestBody, headers map[string]string, scheme string) (*http.Response, error) {
	binding := channelBindingFromState(first.TLS)
	drainClose(first)
	s.attemptedScheme = scheme
	previous := s.authHeader
	defer func() { s.authHeader = previous }()
	leg := func(value string) (*http.Response, error) {
		s.authHeader = value
		return s.send(ctx, method, suffix, contentType, body, headers)
	}
	allowIntegrated := s.settings.emptyAuth != emptyAuthOff
	var rejected *http.Response
	for {
		gen, integrated, err := s.serverGenerator(ctx, scheme, binding, allowIntegrated)
		if err != nil {
			if rejected != nil && errors.Is(err, ErrNoCredentials) {
				return rejected, nil
			}
			closeResponse(rejected)
			return nil, err
		}
		closeResponse(rejected)
		resp, err := runHandshake(gen, headerWWWAuthenticate, scheme, http.StatusUnauthorized, leg)
		gen.close()
		if err != nil && !integrated {
			return nil, err
		}
		if err == nil && resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		rejected = resp
		if integrated {
			allowIntegrated = false
			continue
		}
		s.rejectCredential(ctx, &s.supplied)
		if s.credAttempts >= 2 {
			return rejected, nil
		}
	}
}

func closeResponse(resp *http.Response) {
	if resp != nil {
		drainClose(resp)
	}
}
