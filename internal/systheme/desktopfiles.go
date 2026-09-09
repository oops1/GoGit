package systheme

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func detectFromDesktopFiles(getenv func(string) string, home string) Scheme {
	if s := fromGTKThemeEnv(getenv("GTK_THEME")); s != Unknown {
		return s
	}
	configDir := configHome(getenv, home)
	for _, rel := range []string{"gtk-4.0/settings.ini", "gtk-3.0/settings.ini"} {
		if s := fromGTKSettings(filepath.Join(configDir, rel)); s != Unknown {
			return s
		}
	}
	return fromKDEGlobals(filepath.Join(configDir, "kdeglobals"))
}

func configHome(getenv func(string) string, home string) string {
	if dir := getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".config")
}

func fromGTKThemeEnv(value string) Scheme {
	switch {
	case value == "":
		return Unknown
	case strings.Contains(strings.ToLower(value), "dark"):
		return Dark
	}
	return Light
}

func fromGTKSettings(path string) Scheme {
	values := readINI(path, "Settings")
	if values == nil {
		return Unknown
	}
	switch values["gtk-application-prefer-dark-theme"] {
	case "1", "true":
		return Dark
	case "0", "false":
		if name, ok := values["gtk-theme-name"]; ok {
			return fromThemeName(name)
		}
		return Light
	}
	if name, ok := values["gtk-theme-name"]; ok {
		return fromThemeName(name)
	}
	return Unknown
}

func fromKDEGlobals(path string) Scheme {
	values := readINI(path, "General")
	if values == nil {
		return Unknown
	}
	if name, ok := values["ColorScheme"]; ok {
		return fromThemeName(name)
	}
	return Unknown
}

func fromThemeName(name string) Scheme {
	if strings.Contains(strings.ToLower(name), "dark") {
		return Dark
	}
	return Light
}

func readINI(path, section string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	values := map[string]string{}
	inSection := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = strings.EqualFold(strings.Trim(line, "[]"), section)
			continue
		}
		if !inSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func accentFromDesktopFiles(getenv func(string) string, home string) Accent {
	path := filepath.Join(configHome(getenv, home), "kdeglobals")
	if value, ok := readINI(path, "General")["AccentColor"]; ok {
		if base, ok := parseChannels(value); ok {
			return withShades(base, false)
		}
	}
	if value, ok := readINI(path, "Colors:Selection")["BackgroundNormal"]; ok {
		if base, ok := parseChannels(value); ok {
			return withShades(base, false)
		}
	}
	return Accent{}
}
