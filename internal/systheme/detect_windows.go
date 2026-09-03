package systheme

import (
	"golang.org/x/sys/windows/registry"
)

const personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

func detect() Scheme {
	return fromAppsUseLightTheme(readIntegerValue(personalizeKey, "AppsUseLightTheme"))
}

func readIntegerValue(path, name string) (uint64, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return 0, err
	}
	defer key.Close()
	value, _, err := key.GetIntegerValue(name)
	return value, err
}

func fromAppsUseLightTheme(value uint64, err error) Scheme {
	if err != nil {
		return Unknown
	}
	if value == 0 {
		return Dark
	}
	return Light
}
