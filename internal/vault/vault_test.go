package vault

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

func withFailingAEAD(t *testing.T) {
	t.Helper()
	prev := newXChaCha20Poly1305
	newXChaCha20Poly1305 = func([]byte) (cipher.AEAD, error) {
		return nil, errors.New("vault test: aead construction failed")
	}
	t.Cleanup(func() { newXChaCha20Poly1305 = prev })
}

func createTestVault(t *testing.T, password string) (*Vault, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.bin")
	u := NewPasswordUnlocker([]byte(password), TestSlotParams())
	v, err := Create(context.Background(), Options{Path: path}, u)
	if err != nil {
		t.Fatal(err)
	}
	return v, path
}

func TestCredentialWipe(t *testing.T) {
	c := Credential{Secret: []byte("s")}
	c.Wipe()
	if c.Secret != nil {
		t.Fatal("expected nil after wipe")
	}
}

func TestSSHKeyWipe(t *testing.T) {
	k := SSHKey{Passphrase: []byte("p"), Private: []byte("k")}
	k.Wipe()
	if k.Passphrase != nil || k.Private != nil {
		t.Fatal("expected nil after wipe")
	}
}

func TestCreateOpenUnlockRoundTrip(t *testing.T) {
	v, path := createTestVault(t, "s3cret")
	if err := v.SetCredential(Credential{Resource: "github.com/org", Username: "bob", Secret: []byte("tok")}); err != nil {
		t.Fatal(err)
	}
	v.Lock()

	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Locked() {
		t.Fatal("expected locked")
	}
	if err := reopened.Unlock(context.Background(), NewPasswordUnlocker([]byte("s3cret"), TestSlotParams())); err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Credential("github.com/org/repo")
	if !ok {
		t.Fatal("expected credential via prefix match")
	}
	if got.Username != "bob" || string(got.Secret) != "tok" {
		t.Fatalf("got %+v", got)
	}
}

func TestCreateFailsWhenPathEmpty(t *testing.T) {
	if _, err := Create(context.Background(), Options{}, NewPasswordUnlocker([]byte("p"), TestSlotParams())); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("got %v, want ErrInvalidPath", err)
	}
}

func TestCreateFailsWhenNilUnlocker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.bin")
	if _, err := Create(context.Background(), Options{Path: path}, nil); !errors.Is(err, ErrNilUnlocker) {
		t.Fatalf("got %v, want ErrNilUnlocker", err)
	}
}

func TestCreateFailsWhenAlreadyExists(t *testing.T) {
	_, path := createTestVault(t, "p")
	if _, err := Create(context.Background(), Options{Path: path}, NewPasswordUnlocker([]byte("p"), TestSlotParams())); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("got %v, want ErrAlreadyExists", err)
	}
}

func TestCreateFailsWhenWrapFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.bin")
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	u := NewFileUnlocker(filepath.Join(blocker, "vault.key"))
	if _, err := Create(context.Background(), Options{Path: path}, u); err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateFailsWhenPersistFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.bin")
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	u := NewPasswordUnlocker([]byte("p"), TestSlotParams())
	if _, err := Create(context.Background(), Options{Path: path}, u); err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateFailsWhenStatErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad\x00name")
	u := NewPasswordUnlocker([]byte("p"), TestSlotParams())
	if _, err := Create(context.Background(), Options{Path: path}, u); err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenFailsWhenPathEmpty(t *testing.T) {
	if _, err := Open(Options{}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("got %v, want ErrInvalidPath", err)
	}
}

func TestOpenFailsWhenMissing(t *testing.T) {
	if _, err := Open(Options{Path: filepath.Join(t.TempDir(), "missing.bin")}); err == nil {
		t.Fatal("expected error")
	}
}

func TestPathAndExists(t *testing.T) {
	v, path := createTestVault(t, "p")
	if v.Path() != path {
		t.Fatalf("path mismatch: %s vs %s", v.Path(), path)
	}
	if !v.Exists() {
		t.Fatal("expected exists")
	}
	missing := &Vault{opts: Options{Path: filepath.Join(t.TempDir(), "nope.bin")}}
	if missing.Exists() {
		t.Fatal("expected not exists")
	}
}

func TestSlotsMetadata(t *testing.T) {
	v, _ := createTestVault(t, "p")
	slots := v.Slots()
	if len(slots) != 1 || slots[0].Kind != SlotPassword || slots[0].Index != 0 {
		t.Fatalf("got %+v", slots)
	}
}

func TestLockedGettersReturnFalseOrNil(t *testing.T) {
	_, path := createTestVault(t, "p")
	v, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Credential("x"); ok {
		t.Fatal("expected false")
	}
	if _, ok := v.SSHKey("x"); ok {
		t.Fatal("expected false")
	}
	if v.Resources() != nil {
		t.Fatal("expected nil")
	}
	if v.Hosts() != nil {
		t.Fatal("expected nil")
	}
	if err := v.SetCredential(Credential{Resource: "x"}); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
	if err := v.DeleteCredential("x"); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
	if err := v.SetSSHKey(SSHKey{Host: "x"}); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
	if err := v.DeleteSSHKey("x"); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
	if err := v.AddSlot(context.Background(), NewPasswordUnlocker([]byte("p2"), TestSlotParams())); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
	if err := v.RemoveSlot(0); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestUnlockWrongPassword(t *testing.T) {
	_, path := createTestVault(t, "correct")
	v, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(context.Background(), NewPasswordUnlocker([]byte("wrong"), TestSlotParams())); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestUnlockNilUnlocker(t *testing.T) {
	_, path := createTestVault(t, "p")
	v, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(context.Background(), nil); !errors.Is(err, ErrNilUnlocker) {
		t.Fatalf("got %v", err)
	}
}

func TestUnlockNoMatchingSlot(t *testing.T) {
	_, path := createTestVault(t, "p")
	v, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	err = v.Unlock(context.Background(), NewFileUnlocker(filepath.Join(t.TempDir(), "vault.key")))
	if !errors.Is(err, ErrSlotNotFound) {
		t.Fatalf("got %v, want ErrSlotNotFound", err)
	}
}

func TestUnlockOnClosedVault(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(context.Background(), NewPasswordUnlocker([]byte("p"), TestSlotParams())); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestUnlockCorruptedCiphertextReturnsErrCorrupted(t *testing.T) {
	v, path := createTestVault(t, "p")
	headerBytes, err := encodeHeader(v.version, v.flags, v.slots)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := append([]byte(nil), v.ciphertext...)
	corrupted[len(corrupted)-1] ^= 0xFF
	data := make([]byte, 0, len(headerBytes)+len(v.nonce)+len(corrupted))
	data = append(data, headerBytes...)
	data = append(data, v.nonce...)
	data = append(data, corrupted...)
	v.Lock()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(context.Background(), NewPasswordUnlocker([]byte("p"), TestSlotParams())); !errors.Is(err, ErrCorrupted) {
		t.Fatalf("got %v, want ErrCorrupted", err)
	}
}

func TestUnlockMalformedPayloadJSON(t *testing.T) {
	v, path := createTestVault(t, "p")
	headerBytes, err := encodeHeader(v.version, v.flags, v.slots)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, payloadNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	aead, err := chacha20poly1305.NewX(v.dek)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := aead.Seal(nil, nonce, []byte("not json"), headerBytes)
	data := make([]byte, 0, len(headerBytes)+len(nonce)+len(ciphertext))
	data = append(data, headerBytes...)
	data = append(data, nonce...)
	data = append(data, ciphertext...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	err = reopened.Unlock(context.Background(), NewPasswordUnlocker([]byte("p"), TestSlotParams()))
	if err == nil || errors.Is(err, ErrCorrupted) || errors.Is(err, ErrWrongKey) {
		t.Fatalf("expected a json unmarshal error, got %v", err)
	}
}

func TestCloseThenOperationsFail(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v.SetCredential(Credential{Resource: "x"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if err := v.DeleteCredential("x"); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if err := v.SetSSHKey(SSHKey{Host: "x"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if err := v.DeleteSSHKey("x"); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if err := v.AddSlot(context.Background(), NewPasswordUnlocker([]byte("p2"), TestSlotParams())); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if err := v.RemoveSlot(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestLockZeroesBuffers(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "host", Secret: []byte("topsecret")}); err != nil {
		t.Fatal(err)
	}
	dek := v.dek
	secret := v.secrets.Credentials[0].Secret
	if len(dek) == 0 || len(secret) == 0 {
		t.Fatal("expected non-empty buffers before lock")
	}
	v.Lock()
	for _, b := range dek {
		if b != 0 {
			t.Fatalf("dek not zeroed: %v", dek)
		}
	}
	for _, b := range secret {
		if b != 0 {
			t.Fatalf("secret not zeroed: %v", secret)
		}
	}
	if v.dek != nil {
		t.Fatal("expected v.dek nil after lock")
	}
	if v.secrets != nil {
		t.Fatal("expected v.secrets nil after lock")
	}
}

func TestSetCredentialReplacesExisting(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "host", Username: "a", Secret: []byte("1")}); err != nil {
		t.Fatal(err)
	}
	if err := v.SetCredential(Credential{Resource: "host", Username: "b", Secret: []byte("2")}); err != nil {
		t.Fatal(err)
	}
	got, ok := v.Credential("host")
	if !ok || got.Username != "b" || string(got.Secret) != "2" {
		t.Fatalf("got %+v", got)
	}
	if len(v.secrets.Credentials) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(v.secrets.Credentials))
	}
}

func TestDeleteCredential(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "host", Secret: []byte("1")}); err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteCredential("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if err := v.DeleteCredential("host"); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Credential("host"); ok {
		t.Fatal("expected deleted")
	}
}

func TestCredentialPrefixLookup(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "github.com", Username: "root", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
	got, ok := v.Credential("github.com/org/repo")
	if !ok || got.Username != "root" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if err := v.SetCredential(Credential{Resource: "github.com/org", Username: "org-user", Secret: []byte("s2")}); err != nil {
		t.Fatal(err)
	}
	got, ok = v.Credential("github.com/org/repo")
	if !ok || got.Username != "org-user" {
		t.Fatalf("expected longest prefix match, got %+v", got)
	}
	if _, ok := v.Credential("gitlab.com"); ok {
		t.Fatal("expected no match")
	}
}

func TestResourcesSorted(t *testing.T) {
	v, _ := createTestVault(t, "p")
	for _, r := range []string{"z.example", "a.example", "m.example"} {
		if err := v.SetCredential(Credential{Resource: r, Secret: []byte("s")}); err != nil {
			t.Fatal(err)
		}
	}
	got := v.Resources()
	want := []string{"a.example", "m.example", "z.example"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestSSHKeyExactAndWildcard(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetSSHKey(SSHKey{Host: "*", Path: "~/.ssh/id_ed25519"}); err != nil {
		t.Fatal(err)
	}
	got, ok := v.SSHKey("anyhost")
	if !ok || got.Path != "~/.ssh/id_ed25519" {
		t.Fatalf("got %+v", got)
	}
	if err := v.SetSSHKey(SSHKey{Host: "gitlab.com", Path: "specific"}); err != nil {
		t.Fatal(err)
	}
	got, ok = v.SSHKey("gitlab.com")
	if !ok || got.Path != "specific" {
		t.Fatalf("expected exact match over wildcard, got %+v", got)
	}
}

func TestSetSSHKeyReplaceAndDelete(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetSSHKey(SSHKey{Host: "h", Path: "p1"}); err != nil {
		t.Fatal(err)
	}
	if err := v.SetSSHKey(SSHKey{Host: "h", Path: "p2"}); err != nil {
		t.Fatal(err)
	}
	got, ok := v.SSHKey("h")
	if !ok || got.Path != "p2" {
		t.Fatalf("got %+v", got)
	}
	if err := v.DeleteSSHKey("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if err := v.DeleteSSHKey("h"); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.SSHKey("h"); ok {
		t.Fatal("expected deleted")
	}
}

func TestHosts(t *testing.T) {
	v, _ := createTestVault(t, "p")
	for _, h := range []string{"z", "a"} {
		if err := v.SetSSHKey(SSHKey{Host: h}); err != nil {
			t.Fatal(err)
		}
	}
	got := v.Hosts()
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("got %v", got)
	}
}

func TestAddSlotAndUnlockWithNewSlot(t *testing.T) {
	v, path := createTestVault(t, "primary")
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := v.AddSlot(context.Background(), NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
	v.Lock()

	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(context.Background(), NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
}

func TestAddSlotNilUnlocker(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.AddSlot(context.Background(), nil); !errors.Is(err, ErrNilUnlocker) {
		t.Fatalf("got %v", err)
	}
}

func TestAddSlotTooManySlots(t *testing.T) {
	v, _ := createTestVault(t, "p")
	for i := 0; i < maxSlotCount-1; i++ {
		if err := v.AddSlot(context.Background(), NewFileUnlocker(filepath.Join(t.TempDir(), "vault.key"))); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.AddSlot(context.Background(), NewFileUnlocker(filepath.Join(t.TempDir(), "vault.key"))); !errors.Is(err, ErrTooManySlots) {
		t.Fatalf("got %v, want ErrTooManySlots", err)
	}
}

func TestAddSlotFailsWhenWrapFails(t *testing.T) {
	v, _ := createTestVault(t, "p")
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.AddSlot(context.Background(), NewFileUnlocker(filepath.Join(blocker, "vault.key"))); err == nil {
		t.Fatal("expected error")
	}
	if len(v.slots) != 1 {
		t.Fatalf("slots mutated on failure: %d", len(v.slots))
	}
}

func TestAddSlotPersistFailureLeavesStateUnchanged(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.AddSlot(context.Background(), NewFileUnlocker(filepath.Join(t.TempDir(), "vault.key"))); err == nil {
		t.Fatal("expected error")
	}
	if len(v.slots) != 1 {
		t.Fatalf("slots mutated on failure: %d", len(v.slots))
	}
}

func TestRemoveSlotLastSlotFails(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.RemoveSlot(0); !errors.Is(err, ErrLastSlot) {
		t.Fatalf("got %v, want ErrLastSlot", err)
	}
}

func TestRemoveSlotIndexOutOfRange(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.RemoveSlot(-1); !errors.Is(err, ErrSlotIndex) {
		t.Fatalf("got %v", err)
	}
	if err := v.RemoveSlot(5); !errors.Is(err, ErrSlotIndex) {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveSlotSuccess(t *testing.T) {
	v, path := createTestVault(t, "primary")
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := v.AddSlot(context.Background(), NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
	if err := v.RemoveSlot(0); err != nil {
		t.Fatal(err)
	}
	if slots := v.Slots(); len(slots) != 1 || slots[0].Kind != SlotFile {
		t.Fatalf("unexpected slots: %+v", slots)
	}
	v.Lock()

	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(context.Background(), NewPasswordUnlocker([]byte("primary"), TestSlotParams())); !errors.Is(err, ErrSlotNotFound) {
		t.Fatalf("expected removed password slot to be gone, got %v", err)
	}
	if err := reopened.Unlock(context.Background(), NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveSlotPersistFailureLeavesStateUnchanged(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := v.AddSlot(context.Background(), NewFileUnlocker(filepath.Join(t.TempDir(), "vault.key"))); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.RemoveSlot(0); err == nil {
		t.Fatal("expected error")
	}
	if len(v.slots) != 2 {
		t.Fatalf("slots mutated on failure: %d", len(v.slots))
	}
}

func TestDecryptPayloadLockedRejectsInvalidSlotCount(t *testing.T) {
	v, _ := createTestVault(t, "p")
	v.slots = nil
	if _, err := v.decryptPayloadLocked(v.dek); !errors.Is(err, ErrSlotCount) {
		t.Fatalf("got %v, want ErrSlotCount", err)
	}
}

func TestDecryptPayloadLockedRejectsBadDEKLength(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if _, err := v.decryptPayloadLocked([]byte("short")); err == nil {
		t.Fatal("expected error")
	}
}

func TestPersistWithLockedRejectsInvalidSlotCount(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.persistWithLocked(nil, v.secrets); !errors.Is(err, ErrSlotCount) {
		t.Fatalf("got %v, want ErrSlotCount", err)
	}
}

func TestPersistWithLockedRejectsBadDEKLength(t *testing.T) {
	v, _ := createTestVault(t, "p")
	v.dek = []byte("short")
	if err := v.persistWithLocked(v.slots, v.secrets); err == nil {
		t.Fatal("expected error")
	}
}

func TestOnIdleNoOpWhenAlreadyLocked(t *testing.T) {
	v, _ := createTestVault(t, "p")
	v.Lock()
	v.onIdle()
	if !v.Locked() {
		t.Fatal("expected vault to stay locked")
	}
}

func TestLockZeroesSSHKeySecrets(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetSSHKey(SSHKey{Host: "h", Passphrase: []byte("pass"), Private: []byte("priv")}); err != nil {
		t.Fatal(err)
	}
	passphrase := v.secrets.SSHKeys[0].Passphrase
	private := v.secrets.SSHKeys[0].Private
	v.Lock()
	for _, b := range passphrase {
		if b != 0 {
			t.Fatalf("passphrase not zeroed: %v", passphrase)
		}
	}
	for _, b := range private {
		if b != 0 {
			t.Fatalf("private key not zeroed: %v", private)
		}
	}
}

func TestSetCredentialPersistFailureReturnsError(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.SetCredential(Credential{Resource: "h", Secret: []byte("s")}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteCredentialPersistFailureReturnsError(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "h", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteCredential("h"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetSSHKeyPersistFailureReturnsError(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.SetSSHKey(SSHKey{Host: "h"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteSSHKeyPersistFailureReturnsError(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := v.SetSSHKey(SSHKey{Host: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteSSHKey("h"); err == nil {
		t.Fatal("expected error")
	}
}

func TestIdleTimeoutLocksVault(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "vault.bin")
		v, err := Create(context.Background(), Options{Path: path, IdleTime: 5 * time.Second}, NewPasswordUnlocker([]byte("p"), TestSlotParams()))
		if err != nil {
			t.Fatal(err)
		}
		if v.Locked() {
			t.Fatal("expected unlocked right after create")
		}
		time.Sleep(6 * time.Second)
		synctest.Wait()
		if !v.Locked() {
			t.Fatal("expected vault to auto-lock after idle timeout")
		}
	})
}

func TestIdleTimeoutResetsOnActivity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "vault.bin")
		v, err := Create(context.Background(), Options{Path: path, IdleTime: 5 * time.Second}, NewPasswordUnlocker([]byte("p"), TestSlotParams()))
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
		if err := v.SetCredential(Credential{Resource: "h", Secret: []byte("s")}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if v.Locked() {
			t.Fatal("activity should have reset the idle timer")
		}
	})
}

func TestIdleTimeZeroNeverLocks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "vault.bin")
		v, err := Create(context.Background(), Options{Path: path}, NewPasswordUnlocker([]byte("p"), TestSlotParams()))
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if v.Locked() {
			t.Fatal("IdleTime 0 should never auto-lock")
		}
	})
}
