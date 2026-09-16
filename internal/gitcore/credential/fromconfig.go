package credential

import (
	"errors"
	"fmt"
	"os"

	"github.com/oops1/gogit/internal/gitcore/config"
)

type HelperInfo struct {
	Name      string
	Supported bool
}

type credentialConfig struct {
	query    Query
	helpers  []string
	settings map[string]string
}

func FromConfig(cfg *config.Config, rawURL string) (Chain, []HelperInfo, error) {
	applied, err := applyCredentialConfig(cfg, rawURL)
	if err != nil {
		return nil, nil, err
	}
	env := helperEnvironment{settings: applied.settings, getenv: os.Getenv}
	chain := make(Chain, 0, len(applied.helpers))
	infos := make([]HelperInfo, 0, len(applied.helpers))
	for _, spec := range applied.helpers {
		h, herr := resolveHelper(spec, env)
		if herr != nil {
			if errors.Is(herr, ErrUnsupportedHelper) {
				infos = append(infos, HelperInfo{Name: spec, Supported: false})
				continue
			}
			return nil, nil, herr
		}
		chain = append(chain, h)
		infos = append(infos, HelperInfo{Name: h.Name(), Supported: true})
	}
	return chain, infos, nil
}

func QueryFromConfig(cfg *config.Config, rawURL string) (Query, error) {
	applied, err := applyCredentialConfig(cfg, rawURL)
	if err != nil {
		return Query{}, err
	}
	return applied.query, nil
}

func applyCredentialConfig(cfg *config.Config, rawURL string) (credentialConfig, error) {
	q, err := ParseQuery(rawURL, true)
	if err != nil {
		return credentialConfig{}, err
	}
	usernameFromURL := q.Username != ""
	target, targetOK := normalizeURL(credentialURL(q), false)
	applied := credentialConfig{settings: map[string]string{}}
	useHTTPPath := false
	for e := range cfg.All() {
		if e.Section != "credential" {
			continue
		}
		if e.HasSubsection && !credentialScopeMatches(e.Subsection, target, targetOK, q) {
			continue
		}
		if !e.HasValue {
			return credentialConfig{}, fmt.Errorf("%w: %s", ErrMissingConfigValue, e.Name())
		}
		switch e.Key {
		case "helper":
			if e.Value == "" {
				applied.helpers = applied.helpers[:0]
			} else {
				applied.helpers = append(applied.helpers, e.Value)
			}
		case "username":
			if !usernameFromURL {
				q.Username = e.Value
			}
		case "usehttppath":
			useHTTPPath, err = config.ParseBool(e.Value)
			if err != nil {
				return credentialConfig{}, fmt.Errorf("%s: %w", e.Name(), err)
			}
		default:
			applied.settings[e.Key] = e.Value
		}
	}
	if !useHTTPPath && (q.Protocol == "http" || q.Protocol == "https") {
		q.Path = ""
	}
	applied.query = q
	return applied, nil
}

func credentialScopeMatches(scope string, target urlInfo, targetOK bool, q Query) bool {
	if pattern, ok := normalizeURL(scope, true); ok {
		return targetOK && urlMatches(target, pattern)
	}
	partial, ok := parsePartialCredentialURL(scope)
	return ok && partial.matches(q)
}

type helperEnvironment struct {
	settings map[string]string
	getenv   func(string) string
}

func (env helperEnvironment) setting(envName, key string) string {
	if v := env.getenv(envName); v != "" {
		return v
	}
	return env.settings[key]
}
