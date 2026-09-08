package reposettings

import "strings"

const (
	PullInherit = ""
	PullFF      = "ff"
	PullMerge   = "merge"
	PullRebase  = "rebase"
)

const (
	AutoFetchInherit = ""
	AutoFetchOn      = "on"
	AutoFetchOff     = "off"
)

var pullOrder = []string{PullInherit, PullFF, PullMerge, PullRebase}

var autoFetchOrder = []string{AutoFetchInherit, AutoFetchOn, AutoFetchOff}

var pullKeys = map[string]string{
	PullInherit: "Dialog.RepoSettings.Pull.Inherit",
	PullFF:      "Dialog.RepoSettings.Pull.FF",
	PullMerge:   "Dialog.RepoSettings.Pull.Merge",
	PullRebase:  "Dialog.RepoSettings.Pull.Rebase",
}

var autoFetchKeys = map[string]string{
	AutoFetchInherit: "Dialog.RepoSettings.AutoFetch.Inherit",
	AutoFetchOn:      "Dialog.RepoSettings.AutoFetch.On",
	AutoFetchOff:     "Dialog.RepoSettings.AutoFetch.Off",
}

type Settings struct {
	Name          string
	Path          string
	UserName      string
	UserEmail     string
	DefaultRemote string
	PullStrategy  string
	AutoFetch     string
}

type Inherited struct {
	UserName      string
	UserEmail     string
	DefaultRemote string
	AutoFetch     bool
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

const (
	hintNameRequired = "Dialog.RepoSettings.Hint.NameRequired"
	hintEmailInvalid = "Dialog.RepoSettings.Hint.EmailInvalid"
	hintIdentityHalf = "Dialog.RepoSettings.Hint.IdentityHalf"
	hintReadyToSave  = "Dialog.RepoSettings.Hint.ReadyToSave"
)

func Validate(s Settings) Hint {
	if strings.TrimSpace(s.Name) == "" {
		return Hint{Key: hintNameRequired}
	}
	name := strings.TrimSpace(s.UserName)
	email := strings.TrimSpace(s.UserEmail)
	if email != "" && !looksLikeEmail(email) {
		return Hint{Key: hintEmailInvalid, Args: []any{email}}
	}
	if (name == "") != (email == "") {
		return Hint{Key: hintIdentityHalf}
	}
	return Hint{Key: hintReadyToSave, OK: true}
}

func looksLikeEmail(text string) bool {
	at := strings.IndexByte(text, '@')
	if at <= 0 || at == len(text)-1 {
		return false
	}
	return !strings.ContainsAny(text, " \t") && strings.Contains(text[at+1:], ".")
}

func Normalise(s Settings) Settings {
	s.Name = strings.TrimSpace(s.Name)
	s.UserName = strings.TrimSpace(s.UserName)
	s.UserEmail = strings.TrimSpace(s.UserEmail)
	s.DefaultRemote = strings.TrimSpace(s.DefaultRemote)
	return s
}
