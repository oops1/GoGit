package version

import "runtime/debug"

var Version = "dev"

func String() string {
	info, ok := debug.ReadBuildInfo()
	return resolve(Version, info, ok)
}

func resolve(linked string, info *debug.BuildInfo, ok bool) string {
	if linked != "dev" {
		return linked
	}
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return linked
}
