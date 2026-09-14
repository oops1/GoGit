//go:build linux

package vault

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
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

func (b *fakeSecretBus) openSession(context.Context) (dbus.ObjectPath, error) {
	if b.sessionErr != nil {
		return "", b.sessionErr
	}
	return b.sessionPath, nil
}

func (b *fakeSecretBus) defaultCollection(context.Context) (dbus.ObjectPath, error) {
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

func (b *fakeSecretBus) findItem(_ context.Context, attrs map[string]string) (dbus.ObjectPath, bool, bool, error) {
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

func (b *fakeSecretBus) createItem(_ context.Context, _, _ dbus.ObjectPath, _ string, attrs map[string]string, secret []byte) (dbus.ObjectPath, dbus.ObjectPath, error) {
	switch {
	case b.createErr != nil:
		return "", "", b.createErr
	case b.createNoPrompt:
		return secretServiceNoObject, secretServiceNoObject, nil
	case b.createNeedsPrompt:
		kept := append([]byte(nil), secret...)
		b.pending = func() { b.store(attrs, kept) }
		return secretServiceNoObject, dbus.ObjectPath("/prompt/create"), nil
	case b.dropCreated:
		return itemPathFor(99), secretServiceNoObject, nil
	default:
		return b.store(attrs, secret), secretServiceNoObject, nil
	}
}

func (b *fakeSecretBus) unlock(_ context.Context, objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, error) {
	if b.unlockErr != nil {
		return nil, "", b.unlockErr
	}
	if b.unlockNoPrompt {
		return nil, secretServiceNoObject, nil
	}
	var locked []dbus.ObjectPath
	for _, object := range objects {
		if b.isLocked(object) {
			locked = append(locked, object)
		}
	}
	if len(locked) == 0 {
		return objects, secretServiceNoObject, nil
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

func (b *fakeSecretBus) prompt(context.Context, dbus.ObjectPath) (bool, error) {
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

func (b *fakeSecretBus) readSecret(_ context.Context, _, item dbus.ObjectPath) ([]byte, error) {
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

func (b *fakeSecretBus) close() {
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
		{"no default collection", func(b *fakeSecretBus) { b.collection = secretServiceNoObject }, ErrSlotUnavailable},
		{"empty default collection", func(b *fakeSecretBus) { b.collection = "" }, ErrSlotUnavailable},
		{"unlock call fails", func(b *fakeSecretBus) { b.unlockErr = boom }, boom},
		{"locked collection without prompt", func(b *fakeSecretBus) { b.collectionLocked, b.unlockNoPrompt = true, true }, ErrKeyringLocked},
		{"unlock prompt dismissed", func(b *fakeSecretBus) { b.collectionLocked, b.dismiss = true, true }, ErrKeyringLocked},
		{"unlock prompt fails", func(b *fakeSecretBus) { b.collectionLocked, b.promptErr = true, boom }, boom},
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

func TestPromptCompletionAcceptsOnlyTheAwaitedSignal(t *testing.T) {
	path := dbus.ObjectPath("/prompt/1")
	name := secretServicePrompt + ".Completed"
	cases := []struct {
		name          string
		signal        *dbus.Signal
		wantDismissed bool
		wantOK        bool
	}{
		{"nil", nil, false, false},
		{"other path", &dbus.Signal{Path: "/prompt/2", Name: name, Body: []any{false}}, false, false},
		{"other member", &dbus.Signal{Path: path, Name: "x.Other", Body: []any{false}}, false, false},
		{"empty body", &dbus.Signal{Path: path, Name: name}, false, false},
		{"non bool body", &dbus.Signal{Path: path, Name: name, Body: []any{"yes"}}, false, false},
		{"completed", &dbus.Signal{Path: path, Name: name, Body: []any{false, dbus.MakeVariant("")}}, false, true},
		{"dismissed", &dbus.Signal{Path: path, Name: name, Body: []any{true, dbus.MakeVariant("")}}, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dismissed, ok := promptCompletion(c.signal, path)
			if dismissed != c.wantDismissed || ok != c.wantOK {
				t.Fatalf("got (%v, %v), want (%v, %v)", dismissed, ok, c.wantDismissed, c.wantOK)
			}
		})
	}
}

type dbusFakeItem struct {
	svc *dbusFakeService
	idx int
}

func (it *dbusFakeItem) GetSecret(_ dbus.ObjectPath) (secretServiceSecret, *dbus.Error) {
	it.svc.mu.Lock()
	defer it.svc.mu.Unlock()
	if it.svc.getSecretErr != nil {
		return secretServiceSecret{}, it.svc.getSecretErr
	}
	return it.svc.items[it.idx], nil
}

type dbusFakePrompt struct {
	conn         *dbus.Conn
	path         dbus.ObjectPath
	dismissed    bool
	silent       bool
	garbageFirst bool
	err          *dbus.Error
	onComplete   func()
}

func (p *dbusFakePrompt) Prompt(_ string) *dbus.Error {
	if p.err != nil {
		return p.err
	}
	if p.silent {
		return nil
	}
	go func() {
		if p.garbageFirst {
			_ = p.conn.Emit(p.path, secretServicePrompt+".Completed", "not a bool", dbus.MakeVariant(""))
		}
		if p.onComplete != nil && !p.dismissed {
			p.onComplete()
		}
		_ = p.conn.Emit(p.path, secretServicePrompt+".Completed", p.dismissed, dbus.MakeVariant(secretServiceNoObject))
	}()
	return nil
}

func (p *dbusFakePrompt) Dismiss() *dbus.Error {
	return nil
}

type dbusFakeService struct {
	mu               sync.Mutex
	conn             *dbus.Conn
	attrs            []map[string]string
	items            []secretServiceSecret
	lockedItems      map[int]bool
	collectionLocked bool
	promptDismissed  bool
	promptCount      int
	openSessionErr   *dbus.Error
	aliasErr         *dbus.Error
	searchErr        *dbus.Error
	createErr        *dbus.Error
	getSecretErr     *dbus.Error
	unlockErr        *dbus.Error
}

func (s *dbusFakeService) OpenSession(_ string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openSessionErr != nil {
		return dbus.Variant{}, "", s.openSessionErr
	}
	return dbus.MakeVariant(""), dbus.ObjectPath("/session/1"), nil
}

func (s *dbusFakeService) ReadAlias(_ string) (dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aliasErr != nil {
		return "", s.aliasErr
	}
	return fakeCollectionPath, nil
}

func (s *dbusFakeService) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.searchErr != nil {
		return nil, nil, s.searchErr
	}
	unlocked := []dbus.ObjectPath{}
	locked := []dbus.ObjectPath{}
	for i, a := range s.attrs {
		if !attrsEqual(a, attrs) {
			continue
		}
		if s.lockedItems[i] {
			locked = append(locked, dbusFakeItemPath(i))
		} else {
			unlocked = append(unlocked, dbusFakeItemPath(i))
		}
	}
	return unlocked, locked, nil
}

func (s *dbusFakeService) Unlock(objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unlockErr != nil {
		return nil, "", s.unlockErr
	}
	if !s.collectionLocked && len(s.lockedItems) == 0 {
		return objects, secretServiceNoObject, nil
	}
	s.promptCount++
	path := dbus.ObjectPath("/prompt/" + strconv.Itoa(s.promptCount))
	prompt := &dbusFakePrompt{conn: s.conn, path: path, dismissed: s.promptDismissed, onComplete: func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.collectionLocked = false
		s.lockedItems = nil
	}}
	if err := s.conn.Export(prompt, path, secretServicePrompt); err != nil {
		return nil, "", dbus.MakeFailedError(err)
	}
	return []dbus.ObjectPath{}, path, nil
}

func (s *dbusFakeService) CreateItem(properties map[string]dbus.Variant, secret secretServiceSecret, _ bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	attrsValue, _ := properties["org.freedesktop.Secret.Item.Attributes"].Value().(map[string]string)
	s.mu.Lock()
	if s.createErr != nil {
		err := s.createErr
		s.mu.Unlock()
		return "", "", err
	}
	idx := len(s.items)
	s.items = append(s.items, secret)
	s.attrs = append(s.attrs, attrsValue)
	s.mu.Unlock()
	path := dbusFakeItemPath(idx)
	if err := s.conn.Export(&dbusFakeItem{svc: s, idx: idx}, path, "org.freedesktop.Secret.Item"); err != nil {
		return "", "", dbus.MakeFailedError(err)
	}
	return path, secretServiceNoObject, nil
}

func (s *dbusFakeService) set(update func(*dbusFakeService)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(s)
}

type dbusMalformedOpenSessionService struct{}

func (dbusMalformedOpenSessionService) OpenSession(_ string, _ dbus.Variant) (string, *dbus.Error) {
	return "wrong-shape", nil
}

func dbusFakeItemPath(i int) dbus.ObjectPath {
	return dbus.ObjectPath("/item/" + strconv.Itoa(i))
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

func startFakeDBusSecretService(t *testing.T) *dbusFakeService {
	t.Helper()
	conn := ownSecretServiceBusName(t)
	svc := &dbusFakeService{conn: conn}
	if err := conn.Export(svc, secretServiceObjectPath, secretServiceInterface); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(svc, fakeCollectionPath, "org.freedesktop.Secret.Collection"); err != nil {
		t.Fatal(err)
	}
	return svc
}

func ownSecretServiceBusName(t *testing.T) *dbus.Conn {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	reply, err := conn.RequestName(secretServiceBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		t.Fatal(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("could not own %s: %v", secretServiceBusName, reply)
	}
	return conn
}

func dialTestBus(t *testing.T) *dbusSecretBus {
	t.Helper()
	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bus.close)
	return bus.(*dbusSecretBus)
}

func exportPrompt(t *testing.T, svc *dbusFakeService, prompt *dbusFakePrompt) dbus.ObjectPath {
	t.Helper()
	prompt.conn = svc.conn
	prompt.path = dbus.ObjectPath("/prompt/test")
	if err := svc.conn.Export(prompt, prompt.path, secretServicePrompt); err != nil {
		t.Fatal(err)
	}
	return prompt.path
}

func TestNewDBusSecretBusFailsWhenAddressUnreachable(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/gogit-vault-test.sock")
	if _, err := newDBusSecretBus(); err == nil {
		t.Fatal("expected error when the session bus address is unreachable")
	}
}

func TestDBusSecretBusWireProtocolAgainstFakeService(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	bus := dialTestBus(t)
	ctx := context.Background()

	session, err := bus.openSession(ctx)
	if err != nil || session == "" {
		t.Fatalf("session = %q, err = %v", session, err)
	}
	collection, err := bus.defaultCollection(ctx)
	if err != nil || collection != fakeCollectionPath {
		t.Fatalf("collection = %q, err = %v", collection, err)
	}
	unlocked, prompt, err := bus.unlock(ctx, []dbus.ObjectPath{collection})
	if err != nil || len(unlocked) != 1 || prompt != secretServiceNoObject {
		t.Fatalf("unlock = %v %q %v", unlocked, prompt, err)
	}
	attrs := map[string]string{"application": secretServiceApplication, "label": "gogit-vault", "id": "abc"}
	item, prompt, err := bus.createItem(ctx, collection, session, "gogit-vault", attrs, []byte("secretbytes"))
	if err != nil || item == secretServiceNoObject || prompt != secretServiceNoObject {
		t.Fatalf("create = %q %q %v", item, prompt, err)
	}
	found, locked, ok, err := bus.findItem(ctx, attrs)
	if err != nil || !ok || locked || found != item {
		t.Fatalf("find = %q %v %v %v", found, locked, ok, err)
	}
	got, err := bus.readSecret(ctx, session, item)
	if err != nil || string(got) != "secretbytes" {
		t.Fatalf("read = %q %v", got, err)
	}
	if _, _, ok, err := bus.findItem(ctx, map[string]string{"application": "nope"}); err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
	svc.set(func(s *dbusFakeService) { s.lockedItems = map[int]bool{0: true} })
	if _, locked, ok, err := bus.findItem(ctx, attrs); err != nil || !ok || !locked {
		t.Fatalf("locked find = %v %v %v", locked, ok, err)
	}
}

func TestDBusSecretBusCallsFailWhenServerReturnsErrors(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	boom := dbus.NewError("test.Error", []any{"boom"})
	svc.set(func(s *dbusFakeService) {
		s.openSessionErr, s.aliasErr, s.searchErr, s.createErr, s.unlockErr, s.getSecretErr = boom, boom, boom, boom, boom, boom
	})
	bus := dialTestBus(t)
	ctx := context.Background()
	if _, err := bus.openSession(ctx); err == nil {
		t.Fatal("openSession: expected error")
	}
	if _, err := bus.defaultCollection(ctx); err == nil {
		t.Fatal("defaultCollection: expected error")
	}
	if _, _, _, err := bus.findItem(ctx, map[string]string{}); err == nil {
		t.Fatal("findItem: expected error")
	}
	if _, _, err := bus.createItem(ctx, fakeCollectionPath, "/session/1", "l", map[string]string{}, []byte("s")); err == nil {
		t.Fatal("createItem: expected error")
	}
	if _, _, err := bus.unlock(ctx, []dbus.ObjectPath{fakeCollectionPath}); err == nil {
		t.Fatal("unlock: expected error")
	}
	svc.set(func(s *dbusFakeService) { s.createErr = nil })
	item, _, err := bus.createItem(ctx, fakeCollectionPath, "/session/1", "l", map[string]string{}, []byte("s"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bus.readSecret(ctx, "/session/1", item); err == nil {
		t.Fatal("readSecret: expected error")
	}
}

func TestDBusSecretBusFailsWhenReplyShapeIsWrong(t *testing.T) {
	startPrivateSessionBus(t)
	conn := ownSecretServiceBusName(t)
	if err := conn.Export(dbusMalformedOpenSessionService{}, secretServiceObjectPath, secretServiceInterface); err != nil {
		t.Fatal(err)
	}
	if _, err := dialTestBus(t).openSession(context.Background()); err == nil {
		t.Fatal("expected error decoding a malformed reply")
	}
}

func TestDBusSecretBusPromptReportsCompletionAndDismissal(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	bus := dialTestBus(t)
	for _, dismissed := range []bool{false, true} {
		path := exportPrompt(t, svc, &dbusFakePrompt{dismissed: dismissed, garbageFirst: true})
		got, err := bus.prompt(context.Background(), path)
		if err != nil || got != dismissed {
			t.Fatalf("prompt = %v, %v; want %v", got, err, dismissed)
		}
	}
}

func TestDBusSecretBusPromptFailsWhenThePromptCallFails(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	path := exportPrompt(t, svc, &dbusFakePrompt{err: dbus.NewError("test.Error", []any{"boom"})})
	if _, err := dialTestBus(t).prompt(context.Background(), path); err == nil {
		t.Fatal("expected error")
	}
}

func TestDBusSecretBusPromptTimesOutAsLockedKeyring(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	prev := secretServicePromptTimeout
	secretServicePromptTimeout = 200 * time.Millisecond
	t.Cleanup(func() { secretServicePromptTimeout = prev })
	path := exportPrompt(t, svc, &dbusFakePrompt{silent: true})
	if _, err := dialTestBus(t).prompt(context.Background(), path); !errors.Is(err, ErrKeyringLocked) {
		t.Fatalf("err = %v, want ErrKeyringLocked", err)
	}
}

func TestDBusSecretBusPromptFailsOnAClosedConnection(t *testing.T) {
	startPrivateSessionBus(t)
	startFakeDBusSecretService(t)
	bus := dialTestBus(t)
	bus.close()
	if _, err := bus.prompt(context.Background(), "/prompt/none"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecretServiceUnlockerRoundTripsOverRealDBusWithALockedKeyring(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	svc.set(func(s *dbusFakeService) { s.collectionLocked = true })
	prev := dialSecretBus
	dialSecretBus = newDBusSecretBus
	t.Cleanup(func() { dialSecretBus = prev })

	u := NewSecretServiceUnlocker("gogit-vault")
	slot, err := u.Wrap(context.Background(), testDEK())
	if err != nil {
		t.Fatal(err)
	}
	svc.set(func(s *dbusFakeService) { s.lockedItems = map[int]bool{0: true} })
	got, err := u.Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(testDEK()) {
		t.Fatal("round trip mismatch over real dbus wiring")
	}
	svc.set(func(s *dbusFakeService) {
		s.lockedItems = map[int]bool{0: true}
		s.promptDismissed = true
	})
	if _, err := u.Unwrap(context.Background(), slot); !errors.Is(err, ErrKeyringLocked) {
		t.Fatalf("err = %v, want ErrKeyringLocked after dismissing the unlock prompt", err)
	}
}
