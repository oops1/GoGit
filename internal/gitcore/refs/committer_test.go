package refs

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func configWith(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{NoSystem: true, GlobalFile: path})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestTheCommitterIsTheUserOfTheRepository(t *testing.T) {
	cfg := configWith(t, "[user]\n\tname = Ann\n\temail = ann@example.com\n")

	sig := CommitterFor(cfg)()

	if sig.Name != "Ann" || sig.Email != "ann@example.com" {
		t.Fatalf("committer = %+v, want the user of the repository", sig)
	}
	if sig.When.IsZero() {
		t.Fatal("the committer must be stamped with the time of the update")
	}
}

func TestAHalfConfiguredUserFallsBackToTheAccount(t *testing.T) {
	previous := currentAccount
	currentAccount = func() (string, string) { return "Account", "account@host" }
	t.Cleanup(func() { currentAccount = previous })

	for _, body := range []string{"", "[user]\n\tname = Ann\n", "[user]\n\temail = ann@example.com\n"} {
		cfg := configWith(t, body)
		if sig := CommitterFor(cfg)(); sig.Name != "Account" || sig.Email != "account@host" {
			t.Fatalf("committer = %+v for %q, want the account of the machine", sig, body)
		}
	}
	if sig := CommitterFor(nil)(); sig.Name != "Account" {
		t.Fatalf("committer = %+v without a config, want the account of the machine", sig)
	}
}

func TestTheAccountOfTheMachineAlwaysNamesSomebody(t *testing.T) {
	when := time.Unix(1700000000, 0)

	sig := AccountSignature(when)

	if sig.Name == "" || !strings.Contains(sig.Email, "@") || sig.When != when {
		t.Fatalf("signature = %+v, want a name, an address and the time it was asked for", sig)
	}
}

func TestTheAccountIsBuiltFromWhateverTheMachineTells(t *testing.T) {
	failing := func() (*user.User, error) { return nil, errors.New("no account") }
	noHost := func() (string, error) { return "", errors.New("no hostname") }
	named := func() (*user.User, error) { return &user.User{Name: "Ann Global", Username: "ann"}, nil }
	unnamed := func() (*user.User, error) { return &user.User{Username: "ann"}, nil }
	host := func() (string, error) { return "workstation", nil }

	for _, c := range []struct {
		name      string
		current   func() (*user.User, error)
		hostname  func() (string, error)
		wantName  string
		wantEmail string
	}{
		{"nothing at all", failing, noHost, accountFallbackName, "gogit@localhost"},
		{"a full name", named, host, "Ann Global", "Ann.Global@workstation"},
		{"only a login", unnamed, host, "ann", "ann@workstation"},
		{"an empty hostname", named, func() (string, error) { return "", nil }, "Ann Global", "Ann.Global@localhost"},
	} {
		name, email := accountFrom(c.current, c.hostname)
		if name != c.wantName || email != c.wantEmail {
			t.Fatalf("%s: account = %q <%s>, want %q <%s>", c.name, name, email, c.wantName, c.wantEmail)
		}
	}
}

func TestTheAccountOfThisMachineIsUsable(t *testing.T) {
	name, email := systemAccount()

	if name == "" || !strings.Contains(email, "@") {
		t.Fatalf("account = %q <%s>, want a usable identity", name, email)
	}
}
