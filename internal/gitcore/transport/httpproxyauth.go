package transport

import (
	"context"
	"net/http"
	"net/url"
	"slices"
)

func proxySchemesFor(method string) []string {
	switch method {
	case schemeBasic, schemeNTLM, schemeNegotiate:
		return []string{method}
	case schemeDigest:
		return nil
	default:
		return []string{schemeNegotiate, schemeNTLM, schemeBasic}
	}
}

func selectProxyScheme(challenges map[string]string, method string) string {
	allowed := proxySchemesFor(method)
	if len(allowed) == 1 {
		return allowed[0]
	}
	if len(challenges) == 0 && slices.Contains(allowed, schemeBasic) {
		return schemeBasic
	}
	for _, scheme := range allowed {
		if _, ok := challenges[scheme]; ok {
			return scheme
		}
	}
	return ""
}

func (s *httpSession) plainProxyFor(target string) *url.URL {
	if !s.ownsTransport || requestScheme(target) != string(SchemeHTTP) {
		return nil
	}
	u, err := s.proxy.forURL(target)
	if err != nil {
		return nil
	}
	return u
}

func requestScheme(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Scheme
}

type proxyAttempt struct {
	session         *httpSession
	proxyURL        *url.URL
	binding         []byte
	allowIntegrated bool
	urlUserUsed     map[string]bool
}

func (s *httpSession) proxyAuthenticate(ctx context.Context, proxyURL *url.URL, binding []byte, leg func(string) (*http.Response, error)) (*http.Response, error) {
	method := s.settings.proxyAuthMethod
	a := &proxyAttempt{
		session:         s,
		proxyURL:        proxyURL,
		binding:         binding,
		allowIntegrated: s.settings.emptyAuth != emptyAuthOff,
		urlUserUsed:     map[string]bool{},
	}
	initial := ""
	if proxyURL.User != nil && slices.Contains(proxySchemesFor(method), schemeBasic) {
		initial = basicAuthorization(proxyURL.User)
		a.urlUserUsed[schemeBasic] = true
	}
	var resp *http.Response
	var err error
	if allowed := proxySchemesFor(method); len(allowed) == 1 && allowed[0] != schemeBasic {
		resp, err = a.try(ctx, allowed[0], leg)
	} else {
		resp, err = leg(initial)
	}
	if err != nil {
		return nil, err
	}
	for resp.StatusCode == http.StatusProxyAuthRequired {
		challenge := resp.Header
		drainClose(resp)
		scheme := selectProxyScheme(parseAuthChallenges(challenge, headerProxyAuthenticate), method)
		resp, err = a.try(ctx, scheme, leg)
		if err != nil {
			return nil, err
		}
	}
	s.proxyMu.Lock()
	s.approveCredential(ctx, s.proxySupplied)
	s.proxyMu.Unlock()
	return resp, nil
}

func (a *proxyAttempt) try(ctx context.Context, scheme string, leg func(string) (*http.Response, error)) (*http.Response, error) {
	for {
		var resp *http.Response
		var err error
		integrated := false
		switch scheme {
		case schemeNTLM, schemeNegotiate:
			var gen authGenerator
			gen, integrated, err = a.generator(ctx, scheme)
			if err != nil {
				return nil, err
			}
			resp, err = runHandshake(gen, headerProxyAuthenticate, scheme, http.StatusProxyAuthRequired, leg)
			gen.close()
		case schemeBasic:
			var user *url.Userinfo
			if user, err = a.basicUser(ctx); err != nil {
				return nil, err
			}
			resp, err = leg(basicAuthorization(user))
		default:
			return nil, ErrProxyAuthRequired
		}
		if integrated {
			a.allowIntegrated = false
			if err != nil {
				continue
			}
		}
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusProxyAuthRequired && !integrated {
			a.session.rejectProxyCredential(ctx)
		}
		return resp, nil
	}
}

func (a *proxyAttempt) generator(ctx context.Context, scheme string) (authGenerator, bool, error) {
	if a.allowIntegrated {
		if gen, ok := newIntegratedGenerator(scheme, a.proxyURL.Hostname(), a.binding); ok {
			return gen, true, nil
		}
	}
	username, secret, err := a.credentials(ctx, scheme)
	if err != nil {
		return nil, false, err
	}
	return newCredentialGenerator(scheme, newNTLMCredentials(username, secret), a.binding), false, nil
}

func (a *proxyAttempt) basicUser(ctx context.Context) (*url.Userinfo, error) {
	username, secret, err := a.credentials(ctx, schemeBasic)
	if err != nil {
		return nil, err
	}
	user := url.UserPassword(username, string(secret))
	clear(secret)
	a.session.proxy.authenticate(user)
	return user, nil
}

func (a *proxyAttempt) credentials(ctx context.Context, scheme string) (string, []byte, error) {
	if !a.urlUserUsed[scheme] && a.proxyURL.User != nil {
		a.urlUserUsed[scheme] = true
		password, _ := a.proxyURL.User.Password()
		return a.proxyURL.User.Username(), []byte(password), nil
	}
	return a.session.fetchProxyCredentials(ctx, proxyResource(a.proxyURL))
}

func (s *httpSession) fetchProxyCredentials(ctx context.Context, resource string) (string, []byte, error) {
	s.proxyMu.Lock()
	attempts := s.proxyAttempts
	if s.opts.Credentials == nil || attempts >= 2 {
		s.proxyMu.Unlock()
		return "", nil, ErrProxyAuthRequired
	}
	s.proxyAttempts++
	s.proxyMu.Unlock()
	creds, err := s.opts.Credentials.Credentials(ctx, resource, attempts > 0)
	if err != nil {
		return "", nil, err
	}
	s.proxyMu.Lock()
	forgetCredential(&s.proxySupplied)
	s.proxySupplied = &suppliedCredentials{resource: resource, creds: creds}
	s.proxyMu.Unlock()
	return creds.Username, secretOf(creds), nil
}

func (s *httpSession) rejectProxyCredential(ctx context.Context) {
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	s.rejectCredential(ctx, &s.proxySupplied)
}
