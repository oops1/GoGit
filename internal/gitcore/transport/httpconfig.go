package transport

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

const (
	defaultPostBuffer = 1 << 20
	minPostBuffer     = 65520
)

type httpSettings struct {
	proxy           string
	proxySet        bool
	sslVerify       bool
	caInfo          string
	caPath          string
	sslCert         string
	sslKey          string
	lowSpeedLimit   int64
	lowSpeedTime    int64
	postBuffer      int64
	extraHeaders    []string
	emptyAuth       emptyAuthMode
	proxyAuthMethod string
}

type emptyAuthMode int

const (
	emptyAuthAuto emptyAuthMode = iota
	emptyAuthOn
	emptyAuthOff
)

type urlMatch struct {
	hostLen int
	pathLen int
	user    bool
}

func (m urlMatch) less(other urlMatch) bool {
	if m.hostLen != other.hostLen {
		return m.hostLen < other.hostLen
	}
	if m.pathLen != other.pathLen {
		return m.pathLen < other.pathLen
	}
	return !m.user && other.user
}

func resolveHTTPSettings(cfg *config.Config, remoteName string, target Endpoint) (httpSettings, error) {
	s := httpSettings{sslVerify: true, postBuffer: defaultPostBuffer}
	if cfg != nil {
		if err := s.applyConfig(cfg, target); err != nil {
			return httpSettings{}, err
		}
		if entry, ok := cfg.Lookup("remote." + remoteName + ".proxy"); ok && remoteName != "" {
			s.proxy, s.proxySet = entry.Value, true
		}
		if entry, ok := cfg.Lookup("remote." + remoteName + ".proxyauthmethod"); ok && remoteName != "" {
			s.proxyAuthMethod = strings.ToLower(strings.TrimSpace(entry.Value))
		}
	}
	s.applyEnvironment()
	s.postBuffer = max(s.postBuffer, minPostBuffer)
	return s, nil
}

func (s *httpSettings) applyConfig(cfg *config.Config, target Endpoint) error {
	best := map[string]urlMatch{}
	for entry := range cfg.All() {
		if entry.Section != "http" {
			continue
		}
		match, ok := urlMatch{}, true
		if entry.HasSubsection {
			match, ok = matchConfigURL(entry.Subsection, target)
		}
		key := strings.ToLower(entry.Key)
		if prev, seen := best[key]; !ok || (seen && match.less(prev)) {
			continue
		}
		best[key] = match
		if err := s.apply(key, entry); err != nil {
			return err
		}
	}
	return nil
}

func (s *httpSettings) apply(key string, entry config.Entry) error {
	switch key {
	case "proxy":
		s.proxy, s.proxySet = entry.Value, true
	case "extraheader":
		s.extraHeaders = appendExtraHeader(s.extraHeaders, entry.Value)
	case "sslverify":
		verify, err := config.ParseBool(entry.Value)
		s.sslVerify = verify || !entry.HasValue
		return entryError(entry, err, entry.HasValue)
	case "emptyauth":
		mode, err := parseEmptyAuth(entry)
		s.emptyAuth = mode
		return entryError(entry, err, true)
	case "proxyauthmethod":
		s.proxyAuthMethod = strings.ToLower(strings.TrimSpace(entry.Value))
	case "sslcainfo", "sslcapath", "sslcert", "sslkey":
		expanded, err := config.ExpandPath(entry.Value)
		*s.pathField(key) = expanded
		return entryError(entry, err, true)
	case "lowspeedlimit", "lowspeedtime", "postbuffer":
		n, err := config.ParseInt(entry.Value)
		*s.intField(key) = n
		return entryError(entry, err, true)
	}
	return nil
}

func parseEmptyAuth(entry config.Entry) (emptyAuthMode, error) {
	if !entry.HasValue {
		return emptyAuthOn, nil
	}
	if strings.EqualFold(strings.TrimSpace(entry.Value), "auto") {
		return emptyAuthAuto, nil
	}
	on, err := config.ParseBool(entry.Value)
	switch {
	case err != nil:
		return emptyAuthAuto, err
	case on:
		return emptyAuthOn, nil
	default:
		return emptyAuthOff, nil
	}
}

func appendExtraHeader(headers []string, value string) []string {
	if value == "" {
		return nil
	}
	return append(headers, value)
}

func entryError(entry config.Entry, err error, relevant bool) error {
	if err == nil || !relevant {
		return nil
	}
	return fmt.Errorf("%s: %w", entry.Name(), err)
}

func (s *httpSettings) pathField(key string) *string {
	return map[string]*string{
		"sslcainfo": &s.caInfo,
		"sslcapath": &s.caPath,
		"sslcert":   &s.sslCert,
		"sslkey":    &s.sslKey,
	}[key]
}

func (s *httpSettings) intField(key string) *int64 {
	return map[string]*int64{
		"lowspeedlimit": &s.lowSpeedLimit,
		"lowspeedtime":  &s.lowSpeedTime,
		"postbuffer":    &s.postBuffer,
	}[key]
}

func (s *httpSettings) applyEnvironment() {
	if value, ok := os.LookupEnv("GIT_HTTP_PROXY_AUTHMETHOD"); ok {
		s.proxyAuthMethod = strings.ToLower(strings.TrimSpace(value))
	}
	if _, ok := os.LookupEnv("GIT_SSL_NO_VERIFY"); ok {
		s.sslVerify = false
	}
	for name, field := range map[string]*string{
		"GIT_SSL_CAINFO": &s.caInfo,
		"GIT_SSL_CAPATH": &s.caPath,
		"GIT_SSL_CERT":   &s.sslCert,
		"GIT_SSL_KEY":    &s.sslKey,
	} {
		if value, ok := os.LookupEnv(name); ok {
			*field = value
		}
	}
	for name, field := range map[string]*int64{
		"GIT_HTTP_LOW_SPEED_LIMIT": &s.lowSpeedLimit,
		"GIT_HTTP_LOW_SPEED_TIME":  &s.lowSpeedTime,
	} {
		if value, ok := os.LookupEnv(name); ok {
			*field, _ = strconv.ParseInt(value, 10, 64)
		}
	}
}

func matchConfigURL(pattern string, target Endpoint) (urlMatch, bool) {
	u, err := url.Parse(pattern)
	if err != nil || u.Host == "" || !strings.EqualFold(u.Scheme, string(target.Scheme)) {
		return urlMatch{}, false
	}
	if !hostMatches(u.Hostname(), target.Host) || effectivePort(u.Scheme, u.Port()) != effectivePort(string(target.Scheme), target.Port) {
		return urlMatch{}, false
	}
	match := urlMatch{hostLen: len(u.Hostname())}
	if u.User != nil {
		if u.User.Username() != target.User {
			return urlMatch{}, false
		}
		match.user = true
	}
	prefix := strings.TrimSuffix(u.Path, "/")
	targetPath := strings.TrimSuffix(target.Path, "/")
	if prefix != "" && targetPath != prefix && !strings.HasPrefix(targetPath, prefix+"/") {
		return urlMatch{}, false
	}
	match.pathLen = len(prefix)
	return match, true
}

func hostMatches(pattern, host string) bool {
	patternLabels := strings.Split(strings.ToLower(pattern), ".")
	hostLabels := strings.Split(strings.ToLower(host), ".")
	if len(patternLabels) != len(hostLabels) {
		return false
	}
	for i, label := range patternLabels {
		if ok, err := path.Match(label, hostLabels[i]); err != nil || !ok {
			return false
		}
	}
	return true
}

func effectivePort(scheme, port string) string {
	if port != "" {
		return port
	}
	if strings.EqualFold(scheme, string(SchemeHTTPS)) {
		return "443"
	}
	return "80"
}
