package refs

import (
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	accountFallbackName = "gogit"
	accountFallbackHost = "localhost"
)

var currentAccount = systemAccount

func CommitterFor(cfg *config.Config) func() object.Signature {
	return func() object.Signature {
		now := time.Now()
		if cfg != nil {
			if user := cfg.User(); user.Name != "" && user.Email != "" {
				return object.Signature{Name: user.Name, Email: user.Email, When: now}
			}
		}
		return AccountSignature(now)
	}
}

func AccountSignature(when time.Time) object.Signature {
	name, email := currentAccount()
	return object.Signature{Name: name, Email: email, When: when}
}

func systemAccount() (string, string) {
	return accountFrom(user.Current, os.Hostname)
}

func accountFrom(current func() (*user.User, error), hostname func() (string, error)) (string, string) {
	name := accountFallbackName
	if account, err := current(); err == nil {
		switch {
		case account.Name != "":
			name = account.Name
		case account.Username != "":
			name = account.Username
		}
	}
	host := accountFallbackHost
	if reported, err := hostname(); err == nil && reported != "" {
		host = reported
	}
	return name, strings.ReplaceAll(name, " ", ".") + "@" + host
}
