package submodule

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/index"
)

type pathStyle struct {
	windows bool
}

var nativeStyle = pathStyle{windows: runtime.GOOS == "windows"}

func (s pathStyle) dirSep(c byte) bool {
	return c == '/' || s.windows && c == '\\'
}

func (s pathStyle) dotSlash(path string) bool {
	return len(path) >= 2 && path[0] == '.' && s.dirSep(path[1])
}

func (s pathStyle) dotDotSlash(path string) bool {
	return len(path) >= 3 && path[0] == '.' && s.dotSlash(path[1:])
}

func (s pathStyle) drivePrefix(path string) bool {
	if !s.windows || len(path) < 2 || path[1] != ':' {
		return false
	}
	c := path[0]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (s pathStyle) absolute(path string) bool {
	return path != "" && s.dirSep(path[0]) || s.drivePrefix(path)
}

func (s pathStyle) localNotSSH(url string) bool {
	colon := strings.IndexByte(url, ':')
	slash := strings.IndexByte(url, '/')
	if colon < 0 || slash >= 0 && slash < colon {
		return true
	}
	return s.drivePrefix(url) && index.ValidWin32Path(url[2:])
}

func (s pathStyle) lastDirSep(path string) int {
	for at := len(path) - 1; at >= 0; at-- {
		if s.dirSep(path[at]) {
			return at
		}
	}
	return -1
}

func (s pathStyle) chopLastDir(remote string, relative bool) (string, bool, error) {
	if at := s.lastDirSep(remote); at >= 0 {
		return remote[:at], false, nil
	}
	if at := strings.LastIndexByte(remote, ':'); at >= 0 {
		return remote[:at], true, nil
	}
	if relative || remote == "." {
		return "", false, fmt.Errorf("%w: %q", ErrCannotStripURL, remote)
	}
	return ".", false, nil
}

func (s pathStyle) relativeURL(remoteURL, url, upPath string) (string, error) {
	if !s.localNotSSH(url) || s.absolute(url) {
		return url, nil
	}
	if remoteURL == "" {
		return "", ErrEmptyRemoteURL
	}
	remote := remoteURL
	if s.dirSep(remote[len(remote)-1]) {
		remote = remote[:len(remote)-1]
	}
	relative := s.localNotSSH(remote) && !s.absolute(remote)
	if relative && !s.dotSlash(remote) && !s.dotDotSlash(remote) {
		remote = "./" + remote
	}
	colon := false
	for url != "" {
		if s.dotDotSlash(url) {
			url = url[3:]
			chopped, sep, err := s.chopLastDir(remote, relative)
			if err != nil {
				return "", err
			}
			remote, colon = chopped, colon || sep
			continue
		}
		if !s.dotSlash(url) {
			break
		}
		url = url[2:]
	}
	separator := "/"
	if colon {
		separator = ":"
	}
	out := remote + separator + url
	if strings.HasSuffix(url, "/") {
		out = out[:len(out)-1]
	}
	if s.dotSlash(out) {
		out = out[2:]
	}
	if upPath == "" || !relative {
		return out, nil
	}
	return upPath + out, nil
}

func ResolveRelativeURL(remoteURL, url, upPath string) (string, error) {
	return nativeStyle.relativeURL(remoteURL, url, upPath)
}

func IsRelativeURL(url string) bool {
	crossPlatform := pathStyle{windows: true}
	return crossPlatform.dotSlash(url) || crossPlatform.dotDotSlash(url)
}

func UpPath(path string) string {
	up := strings.Repeat("../", strings.Count(path, "/"))
	if path == "" || !nativeStyle.dirSep(path[len(path)-1]) {
		up += "../"
	}
	return up
}
