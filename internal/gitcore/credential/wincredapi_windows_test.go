package credential

import (
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func realCredentials(t *testing.T) *advapiCredentials {
	t.Helper()
	a, ok := windowsCredentials.(*advapiCredentials)
	if !ok {
		t.Fatalf("windowsCredentials = %T, want *advapiCredentials", windowsCredentials)
	}
	return a
}

func writeOrSkip(t *testing.T, a *advapiCredentials, cred winCredential) {
	t.Helper()
	t.Cleanup(func() { _ = a.remove(cred.target) })
	if err := a.write(cred); err != nil {
		if errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION) {
			t.Skip("the credential manager is not available in this logon session")
		}
		t.Fatal(err)
	}
}

func TestAdvapiCredentialsRoundTripThroughTheCredentialManager(t *testing.T) {
	a := realCredentials(t)
	prefix := "gogit-test:" + strings.ToLower(rand.Text()) + ":"
	target := prefix + "https://example.invalid"
	blob := utf16LEFromUTF8([]byte("пароль"))
	writeOrSkip(t, a, winCredential{target: target, userName: "bob", comment: "gogit test", blob: blob})

	got, found, err := a.read(target)
	if err != nil || !found {
		t.Fatalf("read = %v, %v", found, err)
	}
	if got.target != target || got.userName != "bob" || got.comment != "gogit test" || !slices.Equal(got.blob, blob) {
		t.Fatalf("read = %+v", got)
	}

	listed, err := a.enumerate(prefix + "*")
	if err != nil || len(listed) != 1 || listed[0].target != target {
		t.Fatalf("enumerate(filter) = %+v, %v", listed, err)
	}
	all, err := a.enumerate("")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(all, func(c winCredential) bool { return gcmTrimLegacyPrefix(c.target) == target }) {
		t.Fatal("enumerate(all) does not list the written credential")
	}

	writeOrSkip(t, a, winCredential{target: target})
	emptied, found, err := a.read(target)
	if err != nil || !found || emptied.userName != "" || emptied.comment != "" || len(emptied.blob) != 0 {
		t.Fatalf("read after rewrite = %+v, %v, %v", emptied, found, err)
	}

	if err := a.remove(target); err != nil {
		t.Fatal(err)
	}
	if _, found, err := a.read(target); found || err != nil {
		t.Fatalf("read after remove = %v, %v", found, err)
	}
	if err := a.remove(target); err != nil {
		t.Fatalf("removing a missing credential = %v", err)
	}
	if listed, err := a.enumerate(prefix + "*"); err != nil || len(listed) != 0 {
		t.Fatalf("enumerate after remove = %+v, %v", listed, err)
	}
}

func TestAdvapiCredentialsRejectsInvalidStrings(t *testing.T) {
	a := realCredentials(t)
	bad := "bad\x00target"
	if _, err := a.enumerate(bad); err == nil {
		t.Fatal("enumerate accepted a filter with NUL")
	}
	if _, _, err := a.read(bad); err == nil {
		t.Fatal("read accepted a target with NUL")
	}
	if err := a.remove(bad); err == nil {
		t.Fatal("remove accepted a target with NUL")
	}
	for _, cred := range []winCredential{
		{target: bad},
		{target: "gogit-test:nul", userName: bad},
		{target: "gogit-test:nul", comment: bad},
	} {
		if err := a.write(cred); err == nil {
			t.Fatalf("write accepted %+v", cred)
		}
	}
}

func TestAdvapiCredentialsReportsAPIFailures(t *testing.T) {
	denied := windows.ERROR_ACCESS_DENIED
	a := &advapiCredentials{
		credRead:      func(*uint16, **credentialW) error { return denied },
		credWrite:     func(*credentialW) error { return denied },
		credDelete:    func(*uint16) error { return denied },
		credEnumerate: func(*uint16, uint32, *uint32, ***credentialW) error { return denied },
		credFree:      func(unsafe.Pointer) { t.Fatal("nothing to free after a failed call") },
	}
	if _, err := a.enumerate("git:*"); !errors.Is(err, denied) {
		t.Fatalf("enumerate err = %v", err)
	}
	if _, _, err := a.read("git:x"); !errors.Is(err, denied) {
		t.Fatalf("read err = %v", err)
	}
	if err := a.write(winCredential{target: "git:x"}); !errors.Is(err, denied) {
		t.Fatalf("write err = %v", err)
	}
	if err := a.remove("git:x"); !errors.Is(err, denied) {
		t.Fatalf("remove err = %v", err)
	}
}

func TestAdvapiCredentialsSkipsCredentialsThatAreNotGeneric(t *testing.T) {
	generic := credentialW{Type: credTypeGeneric, TargetName: windows.StringToUTF16Ptr("git:generic")}
	domain := credentialW{Type: 2, TargetName: windows.StringToUTF16Ptr("domain")}
	list := []*credentialW{&domain, &generic}
	freed := 0
	a := &advapiCredentials{
		credEnumerate: func(_ *uint16, flags uint32, count *uint32, creds ***credentialW) error {
			if flags != credEnumerateAllCredentials {
				t.Fatalf("flags = %d, want all credentials for an empty filter", flags)
			}
			*count = uint32(len(list))
			*creds = &list[0]
			return nil
		},
		credFree: func(unsafe.Pointer) { freed++ },
	}
	got, err := a.enumerate("")
	if err != nil || len(got) != 1 || got[0].target != "git:generic" || freed != 1 {
		t.Fatalf("enumerate = %+v, %v, freed %d", got, err, freed)
	}
}

func TestPlatformDefaultsUseTheWindowsCredentialManager(t *testing.T) {
	if windowsCredentials == nil || openKeyring != nil || gcmDefaultStore != gcmStoreWincredman {
		t.Fatal("windows must resolve manager and wincred to the credential manager and have no secret service")
	}
}
