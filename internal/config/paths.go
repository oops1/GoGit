package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const EnvConfigDir = "GOGIT_CONFIG_DIR"

type Paths struct {
	Dir string
}

func DefaultPaths() (Paths, error) {
	if dir := os.Getenv(EnvConfigDir); dir != "" {
		return Paths{Dir: dir}, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{Dir: filepath.Join(base, appDirName(runtime.GOOS))}, nil
}

func appDirName(goos string) string {
	if goos == "windows" {
		return "Go.Git"
	}
	return "gogit"
}

func (p Paths) ConfigFile() string  { return filepath.Join(p.Dir, "config.toml") }
func (p Paths) LayoutFile() string  { return filepath.Join(p.Dir, "layout.json") }
func (p Paths) VaultFile() string   { return filepath.Join(p.Dir, "vault.bin") }
func (p Paths) LogFile() string     { return filepath.Join(p.Dir, "gogit.log") }
func (p Paths) KnownHosts() string  { return filepath.Join(p.Dir, "known_hosts") }
func (p Paths) UserI18NDir() string { return filepath.Join(p.Dir, "i18n") }

func (p Paths) Ensure() error {
	return os.MkdirAll(p.Dir, 0o700)
}
