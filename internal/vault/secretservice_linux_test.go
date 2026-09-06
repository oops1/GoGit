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

type fakeSecretItem struct {
	attrs  map[string]string
	secret []byte
}

type fakeSecretBus struct {
	items       []fakeSecretItem
	sessionErr  error
	searchErr   error
	createErr   error
	readErr     error
	closeCalls  int
	sessionPath dbus.ObjectPath
}

func newFakeSecretBus() *fakeSecretBus {
	return &fakeSecretBus{sessionPath: dbus.ObjectPath("/session/1")}
}

func (b *fakeSecretBus) openSession() (dbus.ObjectPath, error) {
	if b.sessionErr != nil {
		return "", b.sessionErr
	}
	return b.sessionPath, nil
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

func (b *fakeSecretBus) findItem(attrs map[string]string) (dbus.ObjectPath, bool, error) {
	if b.searchErr != nil {
		return "", false, b.searchErr
	}
	for i, it := range b.items {
		if attrsEqual(it.attrs, attrs) {
			return dbus.ObjectPath(itemPathFor(i)), true, nil
		}
	}
	return "", false, nil
}

func itemPathFor(i int) string {
	return "/item/" + string(rune('a'+i))
}

func (b *fakeSecretBus) createItem(_ dbus.ObjectPath, _ string, attrs map[string]string, secret []byte) error {
	if b.createErr != nil {
		return b.createErr
	}
	b.items = append(b.items, fakeSecretItem{attrs: attrs, secret: append([]byte(nil), secret...)})
	return nil
}

func (b *fakeSecretBus) readSecret(_, item dbus.ObjectPath) ([]byte, error) {
	if b.readErr != nil {
		return nil, b.readErr
	}
	for i, it := range b.items {
		if dbus.ObjectPath(itemPathFor(i)) == item {
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

func TestSecretServiceUnlockerRoundTrip(t *testing.T) {
	withFakeSecretBus(t, newFakeSecretBus())
	u := NewSecretServiceUnlocker("gogit-vault")
	dek := []byte("0123456789abcdef0123456789abcdef")[:32]
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	if slot.Kind != SlotSecretService {
		t.Fatalf("kind = %v", slot.Kind)
	}
	got, err := u.Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dek) {
		t.Fatal("unwrapped dek mismatch")
	}
}

func TestSecretServiceUnlockerReopensExistingItem(t *testing.T) {
	bus := newFakeSecretBus()
	withFakeSecretBus(t, bus)
	u := NewSecretServiceUnlocker("gogit-vault")
	dek := make([]byte, 32)
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	if len(bus.items) != 1 {
		t.Fatalf("items = %d, want 1", len(bus.items))
	}
	got, err := NewSecretServiceUnlocker("gogit-vault").Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dek) {
		t.Fatal("reopened dek mismatch")
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

func TestSecretServiceUnlockerWrapsBusErrors(t *testing.T) {
	sessionErr := errors.New("session failed")
	bus := newFakeSecretBus()
	bus.sessionErr = sessionErr
	withFakeSecretBus(t, bus)
	u := NewSecretServiceUnlocker("gogit-vault")
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); !errors.Is(err, sessionErr) {
		t.Fatalf("got %v, want wrapped %v", err, sessionErr)
	}
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotSecretService}); !errors.Is(err, sessionErr) {
		t.Fatalf("got %v, want wrapped %v", err, sessionErr)
	}
}

func TestSecretServiceUnlockerCreateItemErrorPropagates(t *testing.T) {
	createErr := errors.New("create failed")
	bus := newFakeSecretBus()
	bus.createErr = createErr
	withFakeSecretBus(t, bus)
	u := NewSecretServiceUnlocker("gogit-vault")
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); !errors.Is(err, createErr) {
		t.Fatalf("got %v, want wrapped %v", err, createErr)
	}
}

func TestSecretServiceUnlockerSearchErrorPropagates(t *testing.T) {
	searchErr := errors.New("search failed")
	bus := newFakeSecretBus()
	bus.searchErr = searchErr
	withFakeSecretBus(t, bus)
	u := NewSecretServiceUnlocker("gogit-vault")
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotSecretService}); !errors.Is(err, searchErr) {
		t.Fatalf("got %v, want wrapped %v", err, searchErr)
	}
}

func TestSecretServiceUnlockerReadSecretErrorPropagates(t *testing.T) {
	readErr := errors.New("read failed")
	bus := newFakeSecretBus()
	withFakeSecretBus(t, bus)
	u := NewSecretServiceUnlocker("gogit-vault")
	dek := make([]byte, 32)
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	bus.readErr = readErr
	if _, err := u.Unwrap(context.Background(), slot); !errors.Is(err, readErr) {
		t.Fatalf("got %v, want wrapped %v", err, readErr)
	}
}

func TestSecretServiceUnlockerMissingItemIsWrongKey(t *testing.T) {
	withFakeSecretBus(t, newFakeSecretBus())
	slot := Slot{
		Kind:  SlotSecretService,
		Salt:  []byte{1, 2, 3, 4},
		Nonce: make([]byte, slotNonceSize),
	}
	u := NewSecretServiceUnlocker("gogit-vault")
	if _, err := u.Unwrap(context.Background(), slot); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestSecretServiceUnlockerCorruptedWrapperIsWrongKey(t *testing.T) {
	bus := newFakeSecretBus()
	withFakeSecretBus(t, bus)
	u := NewSecretServiceUnlocker("gogit-vault")
	dek := make([]byte, 32)
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	slot.Wrapped = append([]byte(nil), slot.Wrapped...)
	slot.Wrapped[0] ^= 0xFF
	if _, err := u.Unwrap(context.Background(), slot); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestSecretServiceUnlockerUnwrapFailsWhenStoredKeyHasWrongSize(t *testing.T) {
	bus := newFakeSecretBus()
	withFakeSecretBus(t, bus)
	u := NewSecretServiceUnlocker("gogit-vault")
	slot, err := u.Wrap(context.Background(), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	bus.items[0].secret = []byte("too short")
	if _, err := u.Unwrap(context.Background(), slot); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecretServiceUnlockerWrapFailsWhenAEADConstructionFails(t *testing.T) {
	withFakeSecretBus(t, newFakeSecretBus())
	withFailingAEAD(t)
	u := NewSecretServiceUnlocker("gogit-vault")
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecretServiceUnlockerRejectsKindMismatch(t *testing.T) {
	u := NewSecretServiceUnlocker("gogit-vault")
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotFile}); !errors.Is(err, ErrSlotKindMismatch) {
		t.Fatalf("got %v, want ErrSlotKindMismatch", err)
	}
}

func TestSecretServiceUnlockerKind(t *testing.T) {
	if NewSecretServiceUnlocker("gogit-vault").Kind() != SlotSecretService {
		t.Fatal("wrong kind")
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

type dbusFakeService struct {
	mu             sync.Mutex
	conn           *dbus.Conn
	attrs          []map[string]string
	items          []secretServiceSecret
	openSessionErr *dbus.Error
	searchErr      *dbus.Error
	createErr      *dbus.Error
	getSecretErr   *dbus.Error
}

func (s *dbusFakeService) OpenSession(_ string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openSessionErr != nil {
		return dbus.Variant{}, "", s.openSessionErr
	}
	return dbus.MakeVariant(""), dbus.ObjectPath("/session/1"), nil
}

func (s *dbusFakeService) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.searchErr != nil {
		return nil, nil, s.searchErr
	}
	var unlocked []dbus.ObjectPath
	for i, a := range s.attrs {
		if attrsEqual(a, attrs) {
			unlocked = append(unlocked, dbusFakeItemPath(i))
		}
	}
	return unlocked, nil, nil
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
	return path, dbus.ObjectPath("/"), nil
}

type dbusMalformedOpenSessionService struct{}

func (dbusMalformedOpenSessionService) OpenSession(_ string, _ dbus.Variant) (string, *dbus.Error) {
	return "wrong-shape", nil
}

type dbusMalformedSearchItemsService struct{}

func (dbusMalformedSearchItemsService) SearchItems(_ map[string]string) (string, *dbus.Error) {
	return "wrong-shape", nil
}

type dbusMalformedItem struct{}

func (dbusMalformedItem) GetSecret(_ dbus.ObjectPath) (string, *dbus.Error) {
	return "wrong-shape", nil
}

func dbusFakeItemPath(i int) dbus.ObjectPath {
	return dbus.ObjectPath("/item/" + strconv.Itoa(i))
}

func (s *dbusFakeService) setOpenSessionErr(err *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openSessionErr = err
}

func (s *dbusFakeService) setSearchErr(err *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searchErr = err
}

func (s *dbusFakeService) setCreateErr(err *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createErr = err
}

func (s *dbusFakeService) setGetSecretErr(err *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getSecretErr = err
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
	if err := conn.Export(svc, secretServiceObjectPath, "org.freedesktop.Secret.Service"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(svc, secretServiceDefaultAlias, "org.freedesktop.Secret.Collection"); err != nil {
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

func TestNewDBusSecretBusFailsWhenAddressUnreachable(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/gogit-vault-test.sock")
	if _, err := newDBusSecretBus(); err == nil {
		t.Fatal("expected error when the session bus address is unreachable")
	}
}

func TestDBusSecretBusWireProtocolAgainstFakeService(t *testing.T) {
	startPrivateSessionBus(t)
	startFakeDBusSecretService(t)

	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.close()

	session, err := bus.openSession()
	if err != nil {
		t.Fatal(err)
	}
	if session == "" {
		t.Fatal("expected non-empty session path")
	}

	attrs := map[string]string{"application": secretServiceApplication, "label": "gogit-vault", "id": "abc"}
	if err := bus.createItem(session, "gogit-vault", attrs, []byte("secretbytes")); err != nil {
		t.Fatal(err)
	}

	item, ok, err := bus.findItem(attrs)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected item to be found")
	}

	got, err := bus.readSecret(session, item)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secretbytes" {
		t.Fatalf("got %q", got)
	}

	if _, ok, err := bus.findItem(map[string]string{"application": "nope"}); err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
}

func TestDBusSecretBusOpenSessionFailsWhenServerReturnsError(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	svc.setOpenSessionErr(dbus.NewError("test.Error", []interface{}{"boom"}))

	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.close()
	if _, err := bus.openSession(); err == nil {
		t.Fatal("expected error")
	}
}

func TestDBusSecretBusOpenSessionFailsWhenReplyShapeIsWrong(t *testing.T) {
	startPrivateSessionBus(t)
	conn := ownSecretServiceBusName(t)
	if err := conn.Export(dbusMalformedOpenSessionService{}, secretServiceObjectPath, "org.freedesktop.Secret.Service"); err != nil {
		t.Fatal(err)
	}

	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.close()
	if _, err := bus.openSession(); err == nil {
		t.Fatal("expected error decoding a malformed reply")
	}
}

func TestDBusSecretBusFindItemFailsWhenServerReturnsError(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	svc.setSearchErr(dbus.NewError("test.Error", []interface{}{"boom"}))

	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.close()
	if _, _, err := bus.findItem(map[string]string{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDBusSecretBusFindItemFailsWhenReplyShapeIsWrong(t *testing.T) {
	startPrivateSessionBus(t)
	conn := ownSecretServiceBusName(t)
	if err := conn.Export(dbusMalformedSearchItemsService{}, secretServiceObjectPath, "org.freedesktop.Secret.Service"); err != nil {
		t.Fatal(err)
	}

	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.close()
	if _, _, err := bus.findItem(map[string]string{}); err == nil {
		t.Fatal("expected error decoding a malformed reply")
	}
}

func TestDBusSecretBusCreateItemFailsWhenServerReturnsError(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	svc.setCreateErr(dbus.NewError("test.Error", []interface{}{"boom"}))

	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.close()
	if err := bus.createItem("/session/1", "gogit-vault", map[string]string{}, []byte("s")); err == nil {
		t.Fatal("expected error")
	}
}

func TestDBusSecretBusReadSecretFailsWhenServerReturnsError(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeDBusSecretService(t)
	svc.setGetSecretErr(dbus.NewError("test.Error", []interface{}{"boom"}))

	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.close()
	if err := bus.createItem("/session/1", "gogit-vault", map[string]string{"k": "v"}, []byte("s")); err != nil {
		t.Fatal(err)
	}
	item, ok, err := bus.findItem(map[string]string{"k": "v"})
	if err != nil || !ok {
		t.Fatalf("findItem: ok=%v err=%v", ok, err)
	}
	if _, err := bus.readSecret("/session/1", item); err == nil {
		t.Fatal("expected error")
	}
}

func TestDBusSecretBusReadSecretFailsWhenReplyShapeIsWrong(t *testing.T) {
	startPrivateSessionBus(t)
	conn := ownSecretServiceBusName(t)
	if err := conn.Export(dbusMalformedItem{}, dbus.ObjectPath("/item/0"), "org.freedesktop.Secret.Item"); err != nil {
		t.Fatal(err)
	}

	bus, err := newDBusSecretBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.close()
	if _, err := bus.readSecret("/session/1", "/item/0"); err == nil {
		t.Fatal("expected error decoding a malformed reply")
	}
}

func TestSecretServiceUnlockerRoundTripsOverRealDBus(t *testing.T) {
	startPrivateSessionBus(t)
	startFakeDBusSecretService(t)
	prev := dialSecretBus
	dialSecretBus = newDBusSecretBus
	t.Cleanup(func() { dialSecretBus = prev })

	u := NewSecretServiceUnlocker("gogit-vault")
	dek := []byte("0123456789abcdef0123456789abcdef")[:32]
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	got, err := u.Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dek) {
		t.Fatal("round trip mismatch over real dbus wiring")
	}
}
