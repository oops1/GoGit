package transport

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	stdurl "net/url"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrSSHConfig               = errors.New("transport: invalid ssh config")
	ErrProxyCommandUnsupported = errors.New("transport: ProxyCommand is not supported, use ProxyJump")

	errUnterminatedQuote = errors.New("unterminated quote")
)

const maxSSHConfigIncludeDepth = 16

var sshHomeDir = os.UserHomeDir

func DefaultSSHConfigFiles() []string {
	var files []string
	if home, err := sshHomeDir(); err == nil && home != "" {
		files = append(files, filepath.Join(home, ".ssh", "config"))
	}
	if system := systemSSHConfigPath(); system != "" {
		files = append(files, system)
	}
	return files
}

type sshConfigLine struct {
	guards [][]string
	never  bool
	key    string
	args   []string
}

type sshConfigScope struct {
	guards [][]string
	never  bool
	depth  int
}

type sshConfig struct {
	lines []sshConfigLine
}

func loadSSHConfig(opts SSHOptions) (*sshConfig, error) {
	cfg := &sshConfig{}
	for i, override := range opts.Overrides {
		if err := cfg.parse([]byte(override), "", fmt.Sprintf("option %d", i+1), sshConfigScope{}); err != nil {
			return nil, err
		}
	}
	for _, path := range opts.ConfigFiles {
		if err := cfg.parseFile(path, sshConfigScope{}); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func (c *sshConfig) parseFile(path string, scope sshConfigScope) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSSHConfig, err)
	}
	return c.parse(data, filepath.Dir(path), path, scope)
}

func (c *sshConfig) parse(data []byte, dir, name string, scope sshConfigScope) error {
	current := scope
	number := 0
	for raw := range strings.Lines(string(data)) {
		number++
		key, args, err := splitSSHConfigLine(raw)
		if err != nil {
			return fmt.Errorf("%w: %s:%d: %w", ErrSSHConfig, name, number, err)
		}
		switch key {
		case "":
		case "host":
			current = sshConfigScope{guards: appendGuard(scope.guards, args), never: scope.never, depth: scope.depth}
		case "match":
			current = sshConfigScope{guards: scope.guards, never: scope.never || !matchesEverything(args), depth: scope.depth}
		case "include":
			if err := c.include(args, dir, current); err != nil {
				return err
			}
		default:
			c.lines = append(c.lines, sshConfigLine{guards: current.guards, never: current.never, key: key, args: args})
		}
	}
	return nil
}

func appendGuard(guards [][]string, patterns []string) [][]string {
	next := make([][]string, 0, len(guards)+1)
	next = append(next, guards...)
	return append(next, patterns)
}

func matchesEverything(args []string) bool {
	return len(args) == 1 && strings.EqualFold(args[0], "all")
}

func (c *sshConfig) include(args []string, dir string, scope sshConfigScope) error {
	if scope.depth >= maxSSHConfigIncludeDepth {
		return fmt.Errorf("%w: Include nests deeper than %d levels", ErrSSHConfig, maxSSHConfigIncludeDepth)
	}
	nested := scope
	nested.depth++
	for _, pattern := range args {
		pattern = expandUserHome(pattern)
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(dir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("%w: Include %q: %w", ErrSSHConfig, pattern, err)
		}
		for _, match := range matches {
			if err := c.parseFile(match, nested); err != nil {
				return err
			}
		}
	}
	return nil
}

func splitSSHConfigLine(line string) (string, []string, error) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return "", nil, nil
	}
	end := strings.IndexAny(line, " \t=")
	if end < 0 {
		return strings.ToLower(line), nil, nil
	}
	key := strings.ToLower(line[:end])
	rest := strings.TrimLeft(line[end:], " \t")
	rest = strings.TrimPrefix(rest, "=")
	args, err := splitSSHConfigArgs(rest)
	return key, args, err
}

func splitSSHConfigArgs(s string) ([]string, error) {
	var args []string
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" || s[0] == '#' {
			return args, nil
		}
		if s[0] == '"' {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				return nil, errUnterminatedQuote
			}
			args = append(args, s[1:end+1])
			s = s[end+2:]
			continue
		}
		end := strings.IndexAny(s, " \t")
		if end < 0 {
			end = len(s)
		}
		args = append(args, s[:end])
		s = s[end:]
	}
}

func guardsMatch(guards [][]string, host string) bool {
	for _, patterns := range guards {
		if !sshHostMatches(patterns, host) {
			return false
		}
	}
	return true
}

func sshHostMatches(patterns []string, host string) bool {
	host = strings.ToLower(host)
	matched := false
	for _, arg := range patterns {
		for pattern := range strings.SplitSeq(strings.ToLower(arg), ",") {
			negated := strings.HasPrefix(pattern, "!")
			pattern = strings.TrimPrefix(pattern, "!")
			if pattern == "" || !wildcardMatch(pattern, host) {
				continue
			}
			if negated {
				return false
			}
			matched = true
		}
	}
	return matched
}

func wildcardMatch(pattern, value string) bool {
	for pattern != "" {
		switch pattern[0] {
		case '*':
			pattern = pattern[1:]
			for i := len(value); i >= 0; i-- {
				if wildcardMatch(pattern, value[i:]) {
					return true
				}
			}
			return false
		case '?':
			if value == "" {
				return false
			}
		default:
			if value == "" || value[0] != pattern[0] {
				return false
			}
		}
		pattern = pattern[1:]
		value = value[1:]
	}
	return value == ""
}

type sshHostSettings struct {
	values        map[string][]string
	identityFiles []string
}

func (c *sshConfig) resolve(host string) sshHostSettings {
	settings := sshHostSettings{values: map[string][]string{}}
	for _, line := range c.lines {
		if line.never || len(line.args) == 0 || !guardsMatch(line.guards, host) {
			continue
		}
		if line.key == "identityfile" {
			settings.identityFiles = append(settings.identityFiles, line.args[0])
			continue
		}
		if _, seen := settings.values[line.key]; !seen {
			settings.values[line.key] = line.args
		}
	}
	return settings
}

func (s sshHostSettings) value(key string) string {
	if args := s.values[key]; len(args) > 0 {
		return args[0]
	}
	return ""
}

type sshHop struct {
	target          SSHTarget
	addr            string
	hostKeyName     string
	knownHostsFiles []string
	strictHostKeys  string
}

func resolveSSHRoute(endpoint Endpoint, opts SSHOptions) ([]sshHop, error) {
	cfg, err := loadSSHConfig(opts)
	if err != nil {
		return nil, err
	}
	final := cfg.resolve(endpoint.Host)
	jump := final.value("proxyjump")
	if strings.EqualFold(jump, "none") {
		jump = ""
	}
	if command := final.value("proxycommand"); jump == "" && command != "" && !strings.EqualFold(command, "none") {
		return nil, fmt.Errorf("%w: host %s", ErrProxyCommandUnsupported, endpoint.Host)
	}
	var hops []sshHop
	if jump != "" {
		for spec := range strings.SplitSeq(jump, ",") {
			jumpEndpoint, err := parseJumpHost(spec)
			if err != nil {
				return nil, err
			}
			hops = append(hops, buildSSHHop(jumpEndpoint, cfg.resolve(jumpEndpoint.Host)))
		}
	}
	return append(hops, buildSSHHop(endpoint, final)), nil
}

func parseJumpHost(spec string) (Endpoint, error) {
	spec = strings.TrimSpace(spec)
	if strings.HasPrefix(spec, "ssh://") {
		u, err := stdurl.Parse(spec)
		if err != nil || u.Hostname() == "" {
			return Endpoint{}, fmt.Errorf("%w: ProxyJump %q", ErrSSHConfig, spec)
		}
		return Endpoint{Scheme: SchemeSSH, User: u.User.Username(), Host: u.Hostname(), Port: u.Port()}, nil
	}
	endpoint := Endpoint{Scheme: SchemeSSH}
	if at := strings.LastIndexByte(spec, '@'); at >= 0 {
		endpoint.User = spec[:at]
		spec = spec[at+1:]
	}
	endpoint.Host = spec
	if host, port, err := net.SplitHostPort(spec); err == nil {
		endpoint.Host = host
		endpoint.Port = port
	}
	if endpoint.Host == "" {
		return Endpoint{}, fmt.Errorf("%w: ProxyJump %q has no host", ErrSSHConfig, spec)
	}
	return endpoint, nil
}

type sshTokens struct {
	home      string
	hostName  string
	localUser string
	user      string
	alias     string
	port      string
}

func buildSSHHop(endpoint Endpoint, settings sshHostSettings) sshHop {
	alias := endpoint.Host
	port := endpoint.Port
	if port == "" {
		port = settings.value("port")
	}
	if port == "" {
		port = defaultSSHPort
	}
	user := endpoint.User
	if user == "" {
		user = settings.value("user")
	}
	user = sshUsername(user)
	home, _ := sshHomeDir()
	tokens := sshTokens{home: home, hostName: alias, localUser: sshUsername(""), user: user, alias: alias, port: port}
	if hostName := settings.value("hostname"); hostName != "" {
		tokens.hostName = expandSSHTokens(hostName, tokens)
	}
	hop := sshHop{
		target: SSHTarget{
			Host:           alias,
			HostName:       tokens.hostName,
			Port:           port,
			User:           user,
			IdentitiesOnly: sshYes(settings.value("identitiesonly")),
		},
		addr:           net.JoinHostPort(tokens.hostName, port),
		strictHostKeys: strings.ToLower(settings.value("stricthostkeychecking")),
	}
	hop.hostKeyName = hop.addr
	if hostKeyAlias := settings.value("hostkeyalias"); hostKeyAlias != "" {
		hop.hostKeyName = net.JoinHostPort(hostKeyAlias, port)
	}
	hop.target.IdentityFiles = expandSSHPaths(settings.identityFiles, tokens)
	hop.knownHostsFiles = expandSSHPaths(settings.values["userknownhostsfile"], tokens)
	return hop
}

func sshYes(value string) bool {
	return strings.EqualFold(value, "yes") || strings.EqualFold(value, "true")
}

func expandSSHPaths(paths []string, tokens sshTokens) []string {
	var out []string
	for _, path := range paths {
		if strings.EqualFold(path, "none") {
			continue
		}
		path = expandUserHome(expandSSHTokens(path, tokens))
		if filepath.IsAbs(path) {
			path = filepath.Clean(path)
		} else if tokens.home != "" {
			path = filepath.Join(tokens.home, path)
		}
		out = append(out, path)
	}
	return out
}

func expandSSHTokens(value string, tokens sshTokens) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '%' || i+1 == len(value) {
			b.WriteByte(value[i])
			continue
		}
		i++
		switch value[i] {
		case '%':
			b.WriteByte('%')
		case 'd':
			b.WriteString(tokens.home)
		case 'h':
			b.WriteString(tokens.hostName)
		case 'u':
			b.WriteString(tokens.localUser)
		case 'r':
			b.WriteString(tokens.user)
		case 'n':
			b.WriteString(tokens.alias)
		case 'p':
			b.WriteString(tokens.port)
		default:
			b.WriteByte('%')
			b.WriteByte(value[i])
		}
	}
	return b.String()
}

func expandUserHome(path string) string {
	if path != "~" && (len(path) < 2 || path[0] != '~' || !os.IsPathSeparator(path[1])) {
		return path
	}
	home, err := sshHomeDir()
	if err != nil || home == "" {
		return path
	}
	return filepath.Join(home, path[1:])
}
