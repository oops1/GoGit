package ops

import (
	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/merge"
)

const (
	mergeAttribute       = "merge"
	mergeDefaultKey      = "merge.default"
	mergedAncestorsLabel = "merged common ancestors"
	emptyTreeLabel       = "empty tree"
	emptyConflictList    = "\n# Conflicts:\n"
)

func (m *merger) mergeAttributes(path string, virtual bool) merge.PathAttributes {
	attrs := merge.PathAttributes{MarkerSize: m.wt.markerSizeOf(path)}
	value := m.wt.attrs.Get(path, mergeAttribute)[mergeAttribute]
	switch {
	case value.IsSet():
		return attrs
	case value.IsUnset():
		attrs.Driver = merge.DriverBinary
		return attrs
	}
	name := value.Text()
	if value.IsUnspecified() {
		name, _ = m.r.Config().Get(mergeDefaultKey)
	}
	attrs.Driver, attrs.Name = mergeDriverNamed(m.r.Config(), name, virtual)
	return attrs
}

func mergeDriverNamed(cfg *config.Config, name string, virtual bool) (merge.Driver, string) {
	if name != "" && cfg.Has("merge."+name+".driver") {
		recursive, _ := cfg.Get("merge." + name + ".recursive")
		if virtual && recursive != "" {
			return mergeDriverNamed(cfg, recursive, false)
		}
		return merge.DriverExternal, name
	}
	switch name {
	case "binary":
		return merge.DriverBinary, name
	case "union":
		return merge.DriverUnion, name
	}
	return merge.DriverText, name
}

func baseLabel(bases []hash.ObjectID) string {
	switch len(bases) {
	case 0:
		return emptyTreeLabel
	case 1:
		return abbreviate(bases[0])
	}
	return mergedAncestorsLabel
}
