package remote

import (
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
)

type Remote struct {
	Name     string
	URLs     []string
	PushURLs []string
	Fetch    []refspec.RefSpec
	Push     []refspec.RefSpec
}

func (r Remote) FetchURL() string {
	if len(r.URLs) == 0 {
		return ""
	}
	return r.URLs[0]
}

func (r Remote) PushURL() string {
	if len(r.PushURLs) > 0 {
		return r.PushURLs[0]
	}
	return r.FetchURL()
}

type TagMode uint8

const (
	TagsFollow TagMode = iota
	TagsNone
	TagsAll
)

type Change struct {
	Name    refs.Name
	Old     hash.ObjectID
	New     hash.ObjectID
	Created bool
	Deleted bool
	Forced  bool
}

func Load(cfg *config.Config, name string) (Remote, error) {
	raw, ok := cfg.Remote(name)
	if !ok {
		return Remote{}, fmt.Errorf("%w: %s", ErrNoRemote, name)
	}
	rem, err := fromConfig(raw)
	if err != nil {
		return Remote{}, fmt.Errorf("remote: %s: %w", name, err)
	}
	return rem, nil
}

func List(cfg *config.Config) []Remote {
	raw := cfg.Remotes()
	out := make([]Remote, 0, len(raw))
	for _, entry := range raw {
		rem, err := fromConfig(entry)
		if err != nil {
			continue
		}
		out = append(out, rem)
	}
	return out
}

func fromConfig(raw config.Remote) (Remote, error) {
	fetch, err := refspec.ParseAll(raw.Fetch)
	if err != nil {
		return Remote{}, err
	}
	push, err := refspec.ParseAll(raw.Push)
	if err != nil {
		return Remote{}, err
	}
	return Remote{
		Name:     raw.Name,
		URLs:     raw.URLs,
		PushURLs: raw.PushURLs,
		Fetch:    fetch,
		Push:     push,
	}, nil
}
