package credential

import (
	"errors"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

type HelperInfo struct {
	Name      string
	Supported bool
}

func FromConfig(cfg *config.Config, rawURL string) (Chain, []HelperInfo, error) {
	useHTTPPath, err := cfg.GetBool("credential.usehttppath")
	if err != nil {
		if !errors.Is(err, config.ErrNotFound) {
			return nil, nil, err
		}
		useHTTPPath = false
	}
	q, err := ParseQuery(rawURL, useHTTPPath)
	if err != nil {
		return nil, nil, err
	}
	if q.Username == "" {
		if v, ok := cfg.Get("credential.username"); ok {
			q.Username = v
		}
	}
	specs := collectHelperSpecs(cfg, q)
	chain := make(Chain, 0, len(specs))
	infos := make([]HelperInfo, 0, len(specs))
	for _, spec := range specs {
		h, herr := resolveHelper(spec)
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

func collectHelperSpecs(cfg *config.Config, q Query) []string {
	var specs []string
	for e := range cfg.All() {
		if e.Section != "credential" || e.Key != "helper" {
			continue
		}
		if e.HasSubsection {
			pattern, err := ParseQuery(e.Subsection, true)
			if err != nil || !patternMatches(pattern, q) {
				continue
			}
		}
		if !e.HasValue || e.Value == "" {
			specs = specs[:0]
			continue
		}
		specs = append(specs, e.Value)
	}
	return specs
}

func patternMatches(pattern, q Query) bool {
	if pattern.Protocol != "" && !strings.EqualFold(pattern.Protocol, q.Protocol) {
		return false
	}
	if pattern.Host != "" && !strings.EqualFold(pattern.Host, q.Host) {
		return false
	}
	if pattern.Path != "" && pattern.Path != q.Path {
		return false
	}
	if pattern.Username != "" && pattern.Username != q.Username {
		return false
	}
	return true
}
