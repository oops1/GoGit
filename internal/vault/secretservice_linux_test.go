//go:build linux

package vault

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/oops1/gogit/internal/secretservice"
)

const fakeCollectionPath = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")

type fakeSecretItem struct {
	attrs  map[string]string
	secret []byte
	locked bool
}

type fakeSecretBus struct {
	items             []fakeSecretItem
	sessionErr        error
	collectionErr     error
	searchErr         error
	createErr         error
	readErr           error
	unlockErr         error
	promptErr         error
	collection        dbus.ObjectPath
	collectionLocked  bool
	createNeedsPrompt bool
	createNoPrompt    bool
	dropCreated       bool
	corruptStored     bool
	unlockNoPrompt    bool
	dismiss           bool
	pending           func()
	prompts           int
	closeCalls        int
	sessionPath       dbus.ObjectPath
}

func newFakeSecretBus() *fakeSecretBus {
	return &fakeSecretBus{sessionPath: dbus.ObjectPath("/session/1"), collection: fakeCollectionPath}
}

func (b *fakeSecretBus) OpenSession(context.Context) (dbus.ObjectPath, error) {
	if b.sessionErr != nil {
		return "", b.sessionErr
	}
	return b.sessionPath, nil
}

func (b *fakeSecretBus) DefaultCollection(context.Context) (dbus.ObjectPath, error) {
	if b.collectionErr != nil {
		return "", b.collectionErr
	}
	return b.collection, nil
}

func attrsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func itemPathFor(i int) dbus.ObjectPath {
	return dbus.ObjectPath("/item/" + strconv.Itoa(i))
}

func (b *fakeSecretBus) FindItem(_ context.Context, attrs map[string]string) (dbus.ObjectPath, bool, bool, error) {
	if b.searchErr != nil {
		return "", false, false, b.searchErr
	}
	for i, it := range b.items {
		if attrsEqual(it.attrs, attrs) {
			return itemPathFor(i), it.locked, true, nil
		}
	}
	return "", false, false, nil
}

func (b *fakeSecretBus) store(attrs map[string]string, secret []byte) dbus.ObjectPath {
	kept := append([]byte(nil), secret...)
	if b.corruptStored {
		kept[0] ^= 0xFF
	}
	b.items = append(b.items, fakeSecretItem{attrs: attrs, secret: kept})
	return itemPathFor(len(b.items) - 1)
}

func (b *fakeSecretBus) CreateItem(_ context.Context, _, _ dbus.ObjectPath, _ string, attrs map[string]string, secret []byte, _ string) (dbus.ObjectPath, dbus.ObjectPath, error) {
	switch {
	case b.createErr != nil:
		return "", "", b.createErr
	case b.createNoPrompt:
		return secretservice.NoObject, secretservice.NoObject, nil
	case b.createNeedsPrompt:
		kept := append([]byte(nil), secret...)
		b.pending = func() { b.store(attrs, kept) }
		return secretservice.NoObject, dbus.ObjectPath("/prompt/create"), nil
	case b.dropCreated:
		return itemPathFor(99), secretservice.NoObject, nil
	default:
		return b.store(attrs, secret), secretservice.NoObject, nil
	}
}

func (b *fakeSecretBus) Unlock(_ context.Context, objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, error) {
	if b.unlockErr != nil {
		return nil, "", b.unlockErr
	}
	if b.unlockNoPrompt {
		return nil, secretservice.NoObject, nil
	}
	var locked []dbus.ObjectPath
	for _, object := range objects {
		if b.isLocked(object) {
			locked = append(locked, object)
		}
	}
	if len(locked) == 0 {
		return objects, secretservice.NoObject, nil
	}
	b.pending = func() {
		for _, object := range locked {
			b.setLocked(object, false)
		}
	}
	return nil, dbus.ObjectPath("/prompt/unlock"), nil
}

func (b *fakeSecretBus) isLocked(object dbus.ObjectPath) bool {
	if object == b.collection {
		return b.collectionLocked
	}
	for i, it := range b.items {
		if itemPathFor(i) == object {
			return it.locked
		}
	}
	return false
}

func (b *fakeSecretBus) setLocked(object dbus.ObjectPath, locked bool) {
	if object == b.collection {
		b.collectionLocked = locked
	}
	for i := range b.items {
		if itemPathFor(i) == object {
			b.items[i].locked = locked
		}
	}
}

func (b *fakeSecretBus) Prompt(context.Context, dbus.ObjectPath) (bool, error) {
	b.prompts++
	pending := b.pending
	b.pending = nil
	if b.promptErr != nil {
		return false, b.promptErr
	}
	if b.dismiss {
		return true, nil
	}
	if pending != nil {
		pending()
	}
	return false, nil
}

func (b *fakeSecretBus) GetSecret(_ context.Context, _, item dbus.ObjectPath) ([]byte, error) {
	if b.readErr != nil {
		return nil, b.readErr
	}
	for i, it := range b.items {
		if itemPathFor(i) == item {
			if it.locked {
				return nil, errors.New("item is locked")
			}
			return append([]byte(nil), it.secret...), nil
		}
	}
	return nil, errors.New("item not found")
}

func (b *fakeSecretBus) Close() {
	b.closeCalls++
}

func withFakeSecretBus(t *testing.T, bus *fakeSecretBus) {
	t.Helper()
	prev := dialSecretBus
	dialSecretBus = func() (secretBus, error) { return bus, nil }
	t.Cleanup(func() { dialSecretBus = prev })
}

func withUnavailableSecretBus(t *testing.T) {
	t.Helper()
	prev := dialSecretBus
	dialSecretBus = func() (secretBus, error) { return nil, errors.New("no bus") }
	t.Cleanup(func() { dialSecretBus = prev })
}

func testDEK() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func wrapWithFake(t *testing.T, bus *fakeSecretBus) (Slot, error) {
	t.Helper()
	withFakeSecretBus(t, bus)
	return NewSecretServiceUnlocker("gogit-vault").Wrap(context.Background(), testDEK())
}

func TestSecretServiceUnlockerRoundTrip(t *testing.T) {
	bus := newFakeSecretBus()
	slot, err := wrapWithFake(t, bus)
	if err != nil {
		t.Fatal(err)
	}
	if slot.Kind != SlotSecretService || len(bus.items) != 1 || bus.prompts != 0 {
		t.Fatalf("slot = %+v, items = %d, prompts = %d", slot, len(bus.items), bus.prompts)
	}
	got, err := NewSecretServiceUnlocker("gogit-vault").Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(testDEK()) {
		t.Fatal("unwrapped dek mismatch")
	}
	if bus.closeCalls != 2 {
		t.Fatalf("close calls = %d, want 2", bus.closeCalls)
	}
}

func TestSecretServiceWrapAsksToUnlockALockedCollectionFirst(t *testing.T) {
	bus := newFakeSecretBus()
	bus.collectionLocked = true
	if _, err := wrapWithFake(t, bus); err != nil {
		t.Fatal(err)
	}
	if bus.prompts != 1 || bus.collectionLocked || len(bus.items) != 1 {
		t.Fatalf("prompts = %d, locked = %v, items = %d", bus.prompts, bus.collectionLocked, len(bus.items))
	}
}

func TestSecretServiceWrapWaitsForTheCreatePrompt(t *testing.T) {
	bus := newFakeSecretBus()
	bus.createNeedsPrompt = true
	slot, err := wrapWithFake(t, bus)
	if err != nil {
		t.Fatal(err)
	}
	if bus.prompts != 1 || len(bus.items) != 1 {
		t.Fatalf("prompts = %d, items = %d", bus.prompts, len(bus.items))
	}
	if _, err := NewSecretServiceUnlocker("gogit-vault").Unwrap(context.Background(), slot); err != nil {
		t.Fatal(err)
	}
}

func TestSecretServiceWrapFailures(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name  string
		setup func(*fakeSecretBus)
		want  error
	}{
		{"session fails", func(b *fakeSecretBus) { b.sessionErr = boom }, boom},
		{"alias lookup fails", func(b *fakeSecretBus) { b.collectionErr = boom }, boom},
		{"no default collection", func(b *fakeSecretBus) { b.collection = secretservice.NoObject }, ErrSlotUnavailable},
		{"empty default collection", func(b *fakeSecretBus) { b.collection = "" }, ErrSlotUnavailable},
		{"unlock call fails", func(b *fakeSecretBus) { b.unlockErr = boom }, boom},
		{"locked collection without prompt", func(b *fakeSecretBus) { b.collectionLocked, b.unlockNoPrompt = true, true }, ErrKeyringLocked},
		{"unlock prompt dismissed", func(b *fakeSecretBus) { b.collectionLocked, b.dismiss = true, true }, ErrKeyringLocked},
		{"unlock prompt fails", func(b *fakeSecretBus) { b.collectionLocked, b.promptErr = true, boom }, boom},
		{"unlock prompt times out", func(b *fakeSecretBus) { b.collectionLocked, b.promptErr = true, secretservice.ErrPromptTimeout }, ErrKeyringLocked},
		{"create fails", func(b *fakeSecretBus) { b.createErr = boom }, boom},
		{"create prompt dismissed", func(b *fakeSecretBus) { b.createNeedsPrompt, b.dismiss = true, true }, ErrKeyringLocked},
		{"create returns neither item nor prompt", func(b *fakeSecretBus) { b.createNoPrompt = true }, ErrKeyringLocked},
		{"keyring drops the key", func(b *fakeSecretBus) { b.dropCreated = true }, ErrKeyNotStored},
		{"keyring stores a different key", func(b *fakeSecretBus) { b.corruptStored = true }, ErrKeyNotStored},
		{"read back fails", func(b *fakeSecretBus) { b.readErr = boom }, boom},
		{"search while verifying fails", func(b *fakeSecretBus) { b.searchErr = boom }, boom},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bus := newFakeSecretBus()
			c.setup(bus)
			if _, err := wrapWithFake(t, bus); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestSecretServiceWrapFailsWhenAEADConstructionFails(t *testing.T) {
	withFailingAEAD(t)
	if _, err := wrapWithFake(t, newFakeSecretBus()); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecretServiceUnwrapUnlocksALockedItem(t *testing.T) {
	bus := newFakeSecretBus()
	slot, err := wrapWithFake(t, bus)
	if err != nil {
		t.Fatal(err)
	}
	bus.items[0].locked = true
	got, err := NewSecretServiceUnlocker("gogit-vault").Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(testDEK()) || bus.prompts != 1 {
		t.Fatalf("prompts = %d", bus.prompts)
	}
}

func TestSecretServiceUnwrapFailures(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name  string
		setup func(*fakeSecretBus, *Slot)
		want  error
	}{
		{"session fails", func(b *fakeSecretBus, _ *Slot) { b.sessionErr = boom }, boom},
		{"search fails", func(b *fakeSecretBus, _ *Slot) { b.searchErr = boom }, boom},
		{"item missing", func(b *fakeSecretBus, _ *Slot) { b.items = nil }, ErrWrongKey},
		{"locked item prompt dismissed", func(b *fakeSecretBus, _ *Slot) { b.items[0].locked, b.dismiss = true, true }, ErrKeyringLocked},
		{"locked item unlock fails", func(b *fakeSecretBus, _ *Slot) { b.items[0].locked, b.unlockErr = true, boom }, boom},
		{"read fails", func(b *fakeSecretBus, _ *Slot) { b.readErr = boom }, boom},
		{"wrapper corrupted", func(_ *fakeSecretBus, s *Slot) {
			s.Wrapped = append([]byte(nil), s.Wrapped...)
			s.Wrapped[0] ^= 0xFF
		}, ErrWrongKey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bus := newFakeSecretBus()
			slot, err := wrapWithFake(t, bus)
			if err != nil {
				t.Fatal(err)
			}
			c.setup(bus, &slot)
			if _, err := NewSecretServiceUnlocker("gogit-vault").Unwrap(context.Background(), slot); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestSecretServiceUnwrapFailsWhenStoredKeyHasWrongSize(t *testing.T) {
	bus := newFakeSecretBus()
	slot, err := wrapWithFake(t, bus)
	if err != nil {
		t.Fatal(err)
	}
	bus.items[0].secret = []byte("too short")
	if _, err := NewSecretServiceUnlocker("gogit-vault").Unwrap(context.Background(), slot); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecretServiceUnlockerNoServiceIsUnavailable(t *testing.T) {
	withUnavailableSecretBus(t)
	u := NewSecretServiceUnlocker("gogit-vault")
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("Wrap: got %v, want ErrSlotUnavailable", err)
	}
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotSecretService}); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("Unwrap: got %v, want ErrSlotUnavailable", err)
	}
}

func TestSecretServiceUnlockerRejectsKindMismatch(t *testing.T) {
	u := NewSecretServiceUnlocker("gogit-vault")
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotFile}); !errors.Is(err, ErrSlotKindMismatch) {
		t.Fatalf("got %v, want ErrSlotKindMismatch", err)
	}
	if u.Kind() != SlotSecretService {
		t.Fatal("wrong kind")
	}
}

func TestSecretServiceVaultSurvivesAKeyringThatDropsNewItems(t *testing.T) {
	bus := newFakeSecretBus()
	bus.dropCreated = true
	withFakeSecretBus(t, bus)
	path := t.TempDir() + "/vault.bin"
	if _, err := Create(context.Background(), Options{Path: path}, NewSecretServiceUnlocker("gogit-vault")); !errors.Is(err, ErrKeyNotStored) {
		t.Fatalf("err = %v, want ErrKeyNotStored", err)
	}
	v, _ := createTestVault(t, "p")
	if err := v.AddSlot(context.Background(), NewSecretServiceUnlocker("gogit-vault")); !errors.Is(err, ErrKeyNotStored) {
		t.Fatalf("err = %v, want ErrKeyNotStored", err)
	}
	if v.HasSlot(SlotSecretService) {
		t.Fatal("a slot whose key exists nowhere must not be written")
	}
}

func TestSecretServiceAvailableReflectsSessionOpen(t *testing.T) {
	withFakeSecretBus(t, newFakeSecretBus())
	if !SecretServiceAvailable() {
		t.Fatal("expected the fake bus to report available")
	}
}

func TestSecretServiceAvailableFalseWhenDialFails(t *testing.T) {
	withUnavailableSecretBus(t)
	if SecretServiceAvailable() {
		t.Fatal("expected unavailable when the bus cannot be dialed")
	}
}

func TestSecretServiceAvailableFalseWhenSessionFails(t *testing.T) {
	bus := newFakeSecretBus()
	bus.sessionErr = errors.New("no session")
	withFakeSecretBus(t, bus)
	if SecretServiceAvailable() {
		t.Fatal("expected unavailable when opening a session fails")
	}
}

func startPrivateSessionBus(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon not available")
	}
	cmd := exec.Command("dbus-daemon", "--session", "--print-address", "--nofork")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skip("dbus-daemon failed to start")
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	printed := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, err := stdout.Read(buf)
		if err != nil {
			close(printed)
			return
		}
		printed <- strings.TrimSpace(string(buf[:n]))
	}()
	select {
	case address, ok := <-printed:
		if !ok {
			t.Skip("dbus-daemon printed no address")
		}
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	case <-time.After(15 * time.Second):
		t.Skip("dbus-daemon did not print its address in time")
	}
}

func TestDialDBusSecretBusFailsWhenAddressUnreachable(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/gogit-vault-test.sock")
	if _, err := dialDBusSecretBus(); err == nil {
		t.Fatal("expected error when the session bus address is unreachable")
	}
}

func TestDialDBusSecretBusConnectsToTheSessionBus(t *testing.T) {
	startPrivateSessionBus(t)
	bus, err := dialDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	bus.Close()
}
