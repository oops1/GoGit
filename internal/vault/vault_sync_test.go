package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/oops1/gogit/internal/safefile"
)

type stubUnlocker struct {
	kind         SlotKind
	wrapErr      error
	unwrapErr    error
	unwrapResult []byte
	onWrap       func()
}

func (u *stubUnlocker) Kind() SlotKind {
	return u.kind
}

func (u *stubUnlocker) Wrap(_ context.Context, dek []byte) (Slot, error) {
	if u.onWrap != nil {
		u.onWrap()
	}
	if u.wrapErr != nil {
		return Slot{}, u.wrapErr
	}
	return Slot{Kind: u.kind, Wrapped: append([]byte(nil), dek...)}, nil
}

func (u *stubUnlocker) Unwrap(_ context.Context, slot Slot) ([]byte, error) {
	if u.unwrapErr != nil {
		return nil, u.unwrapErr
	}
	if u.unwrapResult != nil {
		return append([]byte(nil), u.unwrapResult...), nil
	}
	return append([]byte(nil), slot.Wrapped...), nil
}

func testPassword(p string) *PasswordUnlocker {
	return NewPasswordUnlocker([]byte(p), TestSlotParams())
}

func openUnlocked(t *testing.T, path string, u Unlocker) *Vault {
	t.Helper()
	v, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return v
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func blockWrites(t *testing.T, path string) func() {
	t.Helper()
	lock := safefile.LockPath(path)
	if err := os.Remove(lock); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTwoInstancesKeepEachOthersEntries(t *testing.T) {
	first, path := createTestVault(t, "p")
	second := openUnlocked(t, path, testPassword("p"))

	if err := first.SetCredential(Credential{Resource: "a.example", Secret: []byte("1")}); err != nil {
		t.Fatal(err)
	}
	if err := second.SetCredential(Credential{Resource: "b.example", Secret: []byte("2")}); err != nil {
		t.Fatal(err)
	}
	if err := first.SetSSHKey(SSHKey{Host: "h.example"}); err != nil {
		t.Fatal(err)
	}

	third := openUnlocked(t, path, testPassword("p"))
	if got := third.Resources(); !slices.Equal(got, []string{"a.example", "b.example"}) {
		t.Fatalf("resources = %v", got)
	}
	if got := third.Hosts(); !slices.Equal(got, []string{"h.example"}) {
		t.Fatalf("hosts = %v", got)
	}
	if got := first.Resources(); !slices.Equal(got, []string{"a.example", "b.example"}) {
		t.Fatalf("the first instance must see the second one's entry after its own write, got %v", got)
	}
}

func TestInstanceKeepsDeletionsMadeByAnotherInstance(t *testing.T) {
	first, path := createTestVault(t, "p")
	for _, r := range []string{"a", "b"} {
		if err := first.SetCredential(Credential{Resource: r, Secret: []byte("s")}); err != nil {
			t.Fatal(err)
		}
	}
	second := openUnlocked(t, path, testPassword("p"))
	if err := first.DeleteCredential("a"); err != nil {
		t.Fatal(err)
	}
	if err := second.SetCredential(Credential{Resource: "c", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
	if err := second.DeleteCredential("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for an entry removed elsewhere", err)
	}
	third := openUnlocked(t, path, testPassword("p"))
	if got := third.Resources(); !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("resources = %v", got)
	}
}

func TestUnlockPicksUpSlotsAddedByAnotherInstance(t *testing.T) {
	first, path := createTestVault(t, "p")
	second, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := first.AddSlot(context.Background(), NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
	if err := second.Unlock(context.Background(), NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
	if !second.HasSlot(SlotFile) || !second.HasSlot(SlotPassword) || second.HasSlot(SlotDPAPI) {
		t.Fatalf("slots = %+v", second.Slots())
	}
}

func TestRekeyByAnotherInstanceLocksThisOneOnItsNextWrite(t *testing.T) {
	first, path := createTestVault(t, "old")
	second := openUnlocked(t, path, testPassword("old"))
	if err := first.ChangePassword(context.Background(), testPassword("old"), testPassword("new"), nil); err != nil {
		t.Fatal(err)
	}
	if err := second.SetCredential(Credential{Resource: "x", Secret: []byte("s")}); !errors.Is(err, ErrChangedElsewhere) {
		t.Fatalf("err = %v, want ErrChangedElsewhere", err)
	}
	if !second.Locked() {
		t.Fatal("an instance holding a revoked key must lock itself")
	}
	if err := second.Unlock(context.Background(), testPassword("new")); err != nil {
		t.Fatal(err)
	}
	if err := second.SetCredential(Credential{Resource: "x", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejectsAFileOlderThanTheKnownGeneration(t *testing.T) {
	v, path := createTestVault(t, "p")
	gen := v.Generation()
	if _, err := Open(Options{Path: path, KnownGeneration: gen + 1}); !errors.Is(err, ErrRollback) {
		t.Fatalf("err = %v, want ErrRollback", err)
	}
	if _, err := Open(Options{Path: path, KnownGeneration: gen}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejectsAFileThatIsNotAVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.bin")
	if err := os.WriteFile(path, []byte("not a vault"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Path: path}); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("err = %v, want ErrInvalidFormat", err)
	}
}

func TestReplacedOlderFileIsDetectedAsRollback(t *testing.T) {
	v, path := createTestVault(t, "p")
	old := readFileBytes(t, path)
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.SetCredential(Credential{Resource: "y", Secret: []byte("s")}); !errors.Is(err, ErrRollback) {
		t.Fatalf("write err = %v, want ErrRollback", err)
	}
	if err := v.Unlock(context.Background(), testPassword("p")); !errors.Is(err, ErrRollback) {
		t.Fatalf("unlock err = %v, want ErrRollback", err)
	}
	if !bytes.Equal(readFileBytes(t, path), old) {
		t.Fatal("a detected rollback must not be overwritten silently")
	}
}

func TestAcceptGenerationLiftsTheFileAboveTheKnownGeneration(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := v.AcceptGeneration(40); err != nil {
		t.Fatal(err)
	}
	if v.Generation() != 41 {
		t.Fatalf("generation = %d, want 41", v.Generation())
	}
	if _, err := Open(Options{Path: path, KnownGeneration: 41}); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if err := v.AcceptGeneration(50); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	_ = v.Close()
	if err := v.AcceptGeneration(50); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestOnGenerationReportsEveryNewGeneration(t *testing.T) {
	var seen []uint64
	path := filepath.Join(t.TempDir(), "vault.bin")
	opts := Options{Path: path, OnGeneration: func(g uint64) { seen = append(seen, g) }}
	v, err := Create(context.Background(), opts, testPassword("p"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, []uint64{1, 2}) || v.Generation() != 2 {
		t.Fatalf("seen = %v, generation = %d", seen, v.Generation())
	}
}

func TestChangePasswordRevokesTheOldPasswordAndItsContentKey(t *testing.T) {
	ctx := context.Background()
	v, path := createTestVault(t, "old")
	if err := v.SetCredential(Credential{Resource: "x", Username: "u", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
	oldFile, err := parseVaultFile(readFileBytes(t, path))
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := testPassword("old").Unwrap(ctx, oldFile.header.slots[0])
	if err != nil {
		t.Fatal(err)
	}

	if err := v.ChangePassword(ctx, testPassword("old"), testPassword("new"), nil); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(ctx, testPassword("old")); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("old password err = %v, want ErrWrongKey", err)
	}
	if err := reopened.Unlock(ctx, testPassword("new")); err != nil {
		t.Fatal(err)
	}
	if c, ok := reopened.Credential("x"); !ok || string(c.Secret) != "s" {
		t.Fatalf("credential lost: %+v", c)
	}
	newFile, err := parseVaultFile(readFileBytes(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openPayload(newFile, oldKey); !errors.Is(err, ErrCorrupted) {
		t.Fatalf("a key taken from an old copy must not open the new file, err = %v", err)
	}
	if len(newFile.header.slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(newFile.header.slots))
	}
}

func TestRepeatedPasswordChangesNeverRunOutOfSlots(t *testing.T) {
	v, _ := createTestVault(t, "p0")
	current := "p0"
	for i := range maxSlotCount + 2 {
		next := current + "x"
		if err := v.ChangePassword(context.Background(), testPassword(current), testPassword(next), nil); err != nil {
			t.Fatalf("change %d: %v", i, err)
		}
		current = next
	}
	if got := len(v.Slots()); got != 1 {
		t.Fatalf("slots = %d, want 1", got)
	}
}

func TestChangePasswordRequiresTheCurrentPassword(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.ChangePassword(context.Background(), testPassword("wrong"), testPassword("new"), nil); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("err = %v, want ErrWrongKey", err)
	}
	if err := v.ChangePassword(context.Background(), nil, testPassword("new"), nil); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("err = %v, want ErrWrongKey without a current password", err)
	}
}

func TestChangePasswordAddsAPasswordToAVaultWithoutOne(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.bin")
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v, err := Create(ctx, Options{Path: path}, NewFileUnlocker(keyPath))
	if err != nil {
		t.Fatal(err)
	}
	others := []Unlocker{nil, testPassword("ignored"), NewFileUnlocker(keyPath)}
	if err := v.ChangePassword(ctx, nil, testPassword("backup"), others); err != nil {
		t.Fatal(err)
	}
	if !v.HasSlot(SlotFile) || !v.HasSlot(SlotPassword) || len(v.Slots()) != 2 {
		t.Fatalf("slots = %+v", v.Slots())
	}
	openUnlocked(t, path, NewFileUnlocker(keyPath))
	openUnlocked(t, path, testPassword("backup"))
}

func TestChangePasswordNeedsAWorkingUnlockerForEveryOtherSlot(t *testing.T) {
	ctx := context.Background()
	v, _ := createTestVault(t, "p")
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := v.AddSlot(ctx, NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
	if err := v.AddSlot(ctx, NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
	if err := v.ChangePassword(ctx, testPassword("p"), testPassword("q"), nil); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("err = %v, want ErrSlotUnavailable", err)
	}
	stranger := NewFileUnlocker(filepath.Join(t.TempDir(), "other.key"))
	if _, err := stranger.Wrap(ctx, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err := v.ChangePassword(ctx, testPassword("p"), testPassword("q"), []Unlocker{stranger}); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("err = %v, want ErrSlotUnavailable for a key that does not open the slot", err)
	}
	if err := v.ChangePassword(ctx, testPassword("p"), testPassword("q"), []Unlocker{stranger, NewFileUnlocker(keyPath)}); err != nil {
		t.Fatal(err)
	}
	if got := len(v.Slots()); got != 2 {
		t.Fatalf("slots = %d, want the file slot once and the new password", got)
	}
}

func TestChangePasswordGuards(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.ChangePassword(context.Background(), testPassword("p"), nil, nil); !errors.Is(err, ErrNilUnlocker) {
		t.Fatalf("err = %v, want ErrNilUnlocker", err)
	}
	v.Lock()
	if err := v.ChangePassword(context.Background(), testPassword("p"), testPassword("q"), nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	_ = v.Close()
	if err := v.ChangePassword(context.Background(), testPassword("p"), testPassword("q"), nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestRekeyReplacesEverySlotWithTheGivenUnlockers(t *testing.T) {
	ctx := context.Background()
	v, path := createTestVault(t, "p")
	stub := &stubUnlocker{kind: "stub"}
	if err := v.Rekey(ctx, []Unlocker{stub, testPassword("q")}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(ctx, testPassword("p")); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("err = %v, want ErrWrongKey", err)
	}
	if err := reopened.Unlock(ctx, stub); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(ctx, testPassword("q")); err != nil {
		t.Fatal(err)
	}
}

func TestRekeyRejectsBadUnlockersWithoutTouchingTheFile(t *testing.T) {
	ctx := context.Background()
	v, path := createTestVault(t, "p")
	before := readFileBytes(t, path)
	wrapErr := errors.New("wrap failed")
	unwrapErr := errors.New("unwrap failed")
	tooMany := make([]Unlocker, maxSlotCount+1)
	for i := range tooMany {
		tooMany[i] = &stubUnlocker{kind: "stub"}
	}
	cases := []struct {
		name      string
		unlockers []Unlocker
		want      error
	}{
		{"none", nil, ErrSlotCount},
		{"too many", tooMany, ErrSlotCount},
		{"nil entry", []Unlocker{nil}, ErrNilUnlocker},
		{"wrap fails", []Unlocker{&stubUnlocker{kind: "stub", wrapErr: wrapErr}}, wrapErr},
		{"unwrap fails", []Unlocker{&stubUnlocker{kind: "stub", unwrapErr: unwrapErr}}, unwrapErr},
		{"key does not round trip", []Unlocker{&stubUnlocker{kind: "stub", unwrapResult: []byte("different")}}, ErrWrongKey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := v.Rekey(ctx, c.unlockers); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !bytes.Equal(readFileBytes(t, path), before) {
				t.Fatal("the file must stay untouched")
			}
		})
	}
	v.Lock()
	if err := v.Rekey(ctx, []Unlocker{testPassword("q")}); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	_ = v.Close()
	if err := v.Rekey(ctx, []Unlocker{testPassword("q")}); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestRekeyWriteFailureKeepsTheOldKey(t *testing.T) {
	ctx := context.Background()
	v, path := createTestVault(t, "p")
	oldKey := append([]byte(nil), v.dek...)
	unblock := blockWrites(t, path)
	if err := v.Rekey(ctx, []Unlocker{testPassword("q")}); err == nil {
		t.Fatal("expected a write error")
	}
	unblock()
	if !bytes.Equal(v.dek, oldKey) {
		t.Fatal("the content key must stay the same after a failed re-key")
	}
	openUnlocked(t, path, testPassword("p"))
}

func TestRevokeSlotRotatesTheContentKey(t *testing.T) {
	ctx := context.Background()
	v, path := createTestVault(t, "p")
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := v.AddSlot(ctx, NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
	oldKey := append([]byte(nil), v.dek...)
	if err := v.RevokeSlot(ctx, 0, []Unlocker{NewFileUnlocker(keyPath)}); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(v.dek, oldKey) {
		t.Fatal("revoking a slot must rotate the content key")
	}
	if slots := v.Slots(); len(slots) != 1 || slots[0].Kind != SlotFile {
		t.Fatalf("slots = %+v", slots)
	}
	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(ctx, testPassword("p")); !errors.Is(err, ErrSlotNotFound) {
		t.Fatalf("err = %v, want ErrSlotNotFound", err)
	}
	if err := reopened.Unlock(ctx, NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
}

func TestRevokeSlotGuards(t *testing.T) {
	ctx := context.Background()
	v, _ := createTestVault(t, "p")
	if err := v.RevokeSlot(ctx, 0, nil); !errors.Is(err, ErrLastSlot) {
		t.Fatalf("err = %v, want ErrLastSlot", err)
	}
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := v.AddSlot(ctx, NewFileUnlocker(keyPath)); err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{-1, 2} {
		if err := v.RevokeSlot(ctx, index, nil); !errors.Is(err, ErrSlotIndex) {
			t.Fatalf("index %d: err = %v, want ErrSlotIndex", index, err)
		}
	}
	if err := v.RevokeSlot(ctx, 0, nil); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("err = %v, want ErrSlotUnavailable", err)
	}
	v.Lock()
	if err := v.RevokeSlot(ctx, 0, nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	_ = v.Close()
	if err := v.RevokeSlot(ctx, 0, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestSuccessfulSaveZeroesThePreviousPlaintext(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("first")}); err != nil {
		t.Fatal(err)
	}
	retained := v.secrets.Credentials[0].Secret
	if err := v.SetCredential(Credential{Resource: "y", Secret: []byte("second")}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retained, make([]byte, len(retained))) {
		t.Fatalf("previous plaintext not zeroed: %q", retained)
	}
	if c, ok := v.Credential("x"); !ok || string(c.Secret) != "first" {
		t.Fatalf("credential lost: %+v", c)
	}
}

func TestFailedSaveKeepsTheCurrentSecrets(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("first")}); err != nil {
		t.Fatal(err)
	}
	unblock := blockWrites(t, path)
	defer unblock()
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("second")}); err == nil {
		t.Fatal("expected a write error")
	}
	if c, ok := v.Credential("x"); !ok || string(c.Secret) != "first" {
		t.Fatalf("credential = %+v, want the saved value", c)
	}
}

func TestSealFailureDiscardsTheNewCopyAndKeepsTheCurrentOne(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("first")}); err != nil {
		t.Fatal(err)
	}
	withFailingAEAD(t)
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("second")}); err == nil {
		t.Fatal("expected a seal error")
	}
	if c, ok := v.Credential("x"); !ok || string(c.Secret) != "first" {
		t.Fatalf("credential = %+v", c)
	}
}

func TestDiscardZeroesOnlyBuffersTheVaultDoesNotOwn(t *testing.T) {
	v, _ := createTestVault(t, "p")
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("kept")}); err != nil {
		t.Fatal(err)
	}
	discarded := revision{
		dek:     []byte{1, 2, 3},
		secrets: &payload{Credentials: []Credential{{Resource: "tmp", Secret: []byte("tmp")}}},
	}
	secret := discarded.secrets.Credentials[0].Secret
	key := discarded.dek
	v.discardLocked(discarded)
	if !bytes.Equal(secret, make([]byte, len(secret))) || !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatalf("discarded buffers not zeroed: %v %v", secret, key)
	}
	v.discardLocked(revision{dek: v.dek, secrets: v.secrets})
	if c, ok := v.Credential("x"); !ok || string(c.Secret) != "kept" {
		t.Fatalf("the vault's own buffers must survive, got %+v", c)
	}
}

func TestWritesRejectGarbageLeftByAnotherProcess(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.SetCredential(Credential{Resource: "x"}); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("write err = %v, want ErrInvalidFormat", err)
	}
	if err := v.Unlock(context.Background(), testPassword("p")); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("unlock err = %v, want ErrInvalidFormat", err)
	}
}

func TestUnlockFailsWhenTheFileCannotBeRead(t *testing.T) {
	v, path := createTestVault(t, "p")
	v.Lock()
	unblock := blockWrites(t, path)
	defer unblock()
	if err := v.Unlock(context.Background(), testPassword("p")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestWriteRecreatesAFileRemovedByAnotherProcess(t *testing.T) {
	v, path := createTestVault(t, "p")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := v.SetCredential(Credential{Resource: "x", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
	reopened := openUnlocked(t, path, testPassword("p"))
	if _, ok := reopened.Credential("x"); !ok {
		t.Fatal("credential missing after re-creation")
	}
}

func TestCreateRefusesAFileThatAppearsWhileWrapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.bin")
	racer := &stubUnlocker{kind: "stub", onWrap: func() {
		if err := os.WriteFile(path, []byte("other"), 0o600); err != nil {
			t.Error(err)
		}
	}}
	if _, err := Create(context.Background(), Options{Path: path}, racer); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
	if string(readFileBytes(t, path)) != "other" {
		t.Fatal("the other file must stay untouched")
	}
}

func TestUnnumberedFileOpensAndUpgradesOnWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.bin")
	dek := make([]byte, chacha20poly1305.KeySize)
	_, _ = rand.Read(dek)
	slot, err := testPassword("p").Wrap(ctx, dek)
	if err != nil {
		t.Fatal(err)
	}
	headerRaw, err := encodeHeader(header{version: formatVersionUnnumbered, slots: []Slot{slot}})
	if err != nil {
		t.Fatal(err)
	}
	aead, err := chacha20poly1305.NewX(dek)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, payloadNonceSize)
	data := append(append([]byte(nil), headerRaw...), nonce...)
	data = append(data, aead.Seal(nil, nonce, []byte(`{"Credentials":[{"Resource":"x","Secret":"cw=="}]}`), headerRaw)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	v := openUnlocked(t, path, testPassword("p"))
	if v.Generation() != 0 {
		t.Fatalf("generation = %d", v.Generation())
	}
	if c, ok := v.Credential("x"); !ok || string(c.Secret) != "s" {
		t.Fatalf("credential = %+v", c)
	}
	if err := v.SetSSHKey(SSHKey{Host: "h"}); err != nil {
		t.Fatal(err)
	}
	file, err := parseVaultFile(readFileBytes(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if file.header.version != formatVersion || file.header.generation != 1 {
		t.Fatalf("header = %+v", file.header)
	}
}
