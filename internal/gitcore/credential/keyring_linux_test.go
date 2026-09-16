package credential

import (
	"context"
	"errors"
	"maps"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/oops1/gogit/internal/secretservice"
)

const fakeBusCollection = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")

type fakeBusItem struct {
	attrs   map[string]string
	secret  []byte
	locked  bool
	deleted bool
}

type fakeSecretBus struct {
	items             []fakeBusItem
	collection        dbus.ObjectPath
	collectionLocked  bool
	sessionErr        error
	collectionErr     error
	searchErr         error
	createErr         error
	unlockErr         error
	promptErr         error
	secretErr         error
	attributesErr     error
	deleteErr         error
	unlockNoPrompt    bool
	createNeedsPrompt bool
	createNoPrompt    bool
	deleteNeedsPrompt bool
	dismiss           bool
	pending           func()
	sessions          int
	prompts           int
	closes            int
	contentTypes      []string
}

func newFakeSecretBus() *fakeSecretBus {
	return &fakeSecretBus{collection: fakeBusCollection}
}

func fakeBusItemPath(i int) dbus.ObjectPath {
	return dbus.ObjectPath("/item/" + strconv.Itoa(i))
}

func fakeBusItemIndex(path dbus.ObjectPath) int {
	i, _ := strconv.Atoi(strings.TrimPrefix(string(path), "/item/"))
	return i
}

func (b *fakeSecretBus) OpenSession(context.Context) (dbus.ObjectPath, error) {
	b.sessions++
	if b.sessionErr != nil {
		return "", b.sessionErr
	}
	return "/session/1", nil
}

func (b *fakeSecretBus) DefaultCollection(context.Context) (dbus.ObjectPath, error) {
	if b.collectionErr != nil {
		return "", b.collectionErr
	}
	return b.collection, nil
}

func (b *fakeSecretBus) SearchItems(_ context.Context, attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, error) {
	if b.searchErr != nil {
		return nil, nil, b.searchErr
	}
	var unlocked, locked []dbus.ObjectPath
	for i, item := range b.items {
		if item.deleted || !attributesContain(item.attrs, attrs) {
			continue
		}
		if item.locked {
			locked = append(locked, fakeBusItemPath(i))
		} else {
			unlocked = append(unlocked, fakeBusItemPath(i))
		}
	}
	return unlocked, locked, nil
}

func (b *fakeSecretBus) CreateItem(_ context.Context, _, _ dbus.ObjectPath, _ string, attrs map[string]string, secret []byte, contentType string) (dbus.ObjectPath, dbus.ObjectPath, error) {
	b.contentTypes = append(b.contentTypes, contentType)
	item := fakeBusItem{attrs: maps.Clone(attrs), secret: append([]byte(nil), secret...)}
	switch {
	case b.createErr != nil:
		return "", "", b.createErr
	case b.createNoPrompt:
		return secretservice.NoObject, secretservice.NoObject, nil
	case b.createNeedsPrompt:
		b.pending = func() { b.items = append(b.items, item) }
		return secretservice.NoObject, "/prompt/create", nil
	}
	b.items = append(b.items, item)
	return fakeBusItemPath(len(b.items) - 1), secretservice.NoObject, nil
}

func (b *fakeSecretBus) Unlock(_ context.Context, objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, error) {
	if b.unlockErr != nil {
		return nil, "", b.unlockErr
	}
	if b.unlockNoPrompt {
		return nil, secretservice.NoObject, nil
	}
	locked := false
	for _, object := range objects {
		if object == b.collection {
			locked = locked || b.collectionLocked
			continue
		}
		locked = locked || b.items[fakeBusItemIndex(object)].locked
	}
	if !locked {
		return objects, secretservice.NoObject, nil
	}
	b.pending = func() {
		b.collectionLocked = false
		for i := range b.items {
			b.items[i].locked = false
		}
	}
	return nil, "/prompt/unlock", nil
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
	if b.secretErr != nil {
		return nil, b.secretErr
	}
	return append([]byte(nil), b.items[fakeBusItemIndex(item)].secret...), nil
}

func (b *fakeSecretBus) ItemAttributes(_ context.Context, item dbus.ObjectPath) (map[string]string, error) {
	if b.attributesErr != nil {
		return nil, b.attributesErr
	}
	return maps.Clone(b.items[fakeBusItemIndex(item)].attrs), nil
}

func (b *fakeSecretBus) DeleteItem(_ context.Context, item dbus.ObjectPath) (dbus.ObjectPath, error) {
	if b.deleteErr != nil {
		return "", b.deleteErr
	}
	i := fakeBusItemIndex(item)
	if b.deleteNeedsPrompt {
		b.pending = func() { b.items[i].deleted = true }
		return "/prompt/delete", nil
	}
	b.items[i].deleted = true
	return secretservice.NoObject, nil
}

func (b *fakeSecretBus) Close() {
	b.closes++
}

func (b *fakeSecretBus) live() int {
	n := 0
	for _, item := range b.items {
		if !item.deleted {
			n++
		}
	}
	return n
}

func withFakeKeyringBus(t *testing.T, bus *fakeSecretBus) {
	t.Helper()
	prev := dialKeyringBus
	dialKeyringBus = func() (secretServiceBus, error) { return bus, nil }
	t.Cleanup(func() { dialKeyringBus = prev })
}

func TestDBusKeyringStoresSearchesAndRemovesItems(t *testing.T) {
	bus := newFakeSecretBus()
	withFakeKeyringBus(t, bus)
	ctx := context.Background()
	k, err := openDBusKeyring()
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]string{"service": "git:https://github.com", "account": "bob"}
	if err := k.store(ctx, "label", attrs, []byte("pw"), "plain/text"); err != nil {
		t.Fatal(err)
	}
	items, err := k.search(ctx, map[string]string{"service": "git:https://github.com"})
	if err != nil || len(items) != 1 || string(items[0].secret) != "pw" || !maps.Equal(items[0].attributes, attrs) {
		t.Fatalf("search = %+v, %v", items, err)
	}
	if err := k.remove(ctx, items); err != nil {
		t.Fatal(err)
	}
	k.close()
	if bus.live() != 0 || bus.sessions != 1 || bus.closes != 1 || bus.prompts != 0 || bus.contentTypes[0] != "plain/text" {
		t.Fatalf("bus = %+v", bus)
	}
}

func TestDBusKeyringUnlocksThroughPrompts(t *testing.T) {
	bus := newFakeSecretBus()
	bus.collectionLocked = true
	bus.createNeedsPrompt = true
	withFakeKeyringBus(t, bus)
	ctx := context.Background()
	k, _ := openDBusKeyring()
	if err := k.store(ctx, "label", map[string]string{"a": "1"}, []byte("pw"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	bus.items[0].locked = true
	items, err := k.search(ctx, map[string]string{"a": "1"})
	if err != nil || len(items) != 1 || string(items[0].secret) != "pw" {
		t.Fatalf("search = %+v, %v", items, err)
	}
	bus.deleteNeedsPrompt = true
	if err := k.remove(ctx, items); err != nil {
		t.Fatal(err)
	}
	if bus.live() != 0 || bus.prompts != 4 {
		t.Fatalf("live = %d, prompts = %d", bus.live(), bus.prompts)
	}
}

func TestDBusKeyringReportsBusFailures(t *testing.T) {
	boom := errors.New("boom")
	ctx := context.Background()
	attrs := map[string]string{"a": "1"}
	withItem := func(b *fakeSecretBus) {
		b.items = []fakeBusItem{{attrs: map[string]string{"a": "1"}, secret: []byte("pw")}}
	}
	lockedItem := func(b *fakeSecretBus) {
		withItem(b)
		b.items[0].locked = true
	}
	cases := []struct {
		name  string
		setup func(*fakeSecretBus)
		op    string
		want  error
	}{
		{"search session", func(b *fakeSecretBus) { b.sessionErr = boom }, "search", boom},
		{"search call", func(b *fakeSecretBus) { b.searchErr = boom }, "search", boom},
		{"search unlock call", func(b *fakeSecretBus) { lockedItem(b); b.unlockErr = boom }, "search", boom},
		{"search unlock dismissed", func(b *fakeSecretBus) { lockedItem(b); b.dismiss = true }, "search", ErrKeyringLocked},
		{"search unlock without prompt", func(b *fakeSecretBus) { lockedItem(b); b.unlockNoPrompt = true }, "search", ErrKeyringLocked},
		{"search prompt fails", func(b *fakeSecretBus) { lockedItem(b); b.promptErr = boom }, "search", boom},
		{"search attributes", func(b *fakeSecretBus) { withItem(b); b.attributesErr = boom }, "search", boom},
		{"search secret", func(b *fakeSecretBus) { withItem(b); b.secretErr = boom }, "search", boom},
		{"store session", func(b *fakeSecretBus) { b.sessionErr = boom }, "store", boom},
		{"store collection", func(b *fakeSecretBus) { b.collectionErr = boom }, "store", boom},
		{"store without default collection", func(b *fakeSecretBus) { b.collection = secretservice.NoObject }, "store", ErrNoDefaultKeyring},
		{"store unlock", func(b *fakeSecretBus) { b.collectionLocked, b.unlockErr = true, boom }, "store", boom},
		{"store create", func(b *fakeSecretBus) { b.createErr = boom }, "store", boom},
		{"store create without prompt", func(b *fakeSecretBus) { b.createNoPrompt = true }, "store", ErrKeyringLocked},
		{"store create dismissed", func(b *fakeSecretBus) { b.createNeedsPrompt, b.dismiss = true, true }, "store", ErrKeyringLocked},
		{"remove call", func(b *fakeSecretBus) { withItem(b); b.deleteErr = boom }, "remove", boom},
		{"remove dismissed", func(b *fakeSecretBus) { withItem(b); b.deleteNeedsPrompt, b.dismiss = true, true }, "remove", ErrKeyringLocked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bus := newFakeSecretBus()
			c.setup(bus)
			k := &dbusKeyring{bus: bus}
			var err error
			switch c.op {
			case "search":
				_, err = k.search(ctx, attrs)
			case "store":
				err = k.store(ctx, "label", attrs, []byte("pw"), "text/plain")
			case "remove":
				err = k.remove(ctx, []keyringItem{{handle: string(fakeBusItemPath(0))}})
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestOpenDBusKeyringFailsWhenTheBusIsUnavailable(t *testing.T) {
	boom := errors.New("no bus")
	prev := dialKeyringBus
	dialKeyringBus = func() (secretServiceBus, error) { return nil, boom }
	t.Cleanup(func() { dialKeyringBus = prev })
	if _, err := openDBusKeyring(); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestDialSecretServiceBusFailsWhenAddressUnreachable(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/gogit-credential-test.sock")
	if _, err := dialSecretServiceBus(); err == nil {
		t.Fatal("expected error when the session bus address is unreachable")
	}
}

func TestDialSecretServiceBusConnectsToTheSessionBus(t *testing.T) {
	startPrivateSessionBus(t)
	bus, err := dialSecretServiceBus()
	if err != nil {
		t.Fatal(err)
	}
	bus.Close()
}

func TestPlatformDefaultsUseTheSecretService(t *testing.T) {
	if windowsCredentials != nil || openKeyring == nil || gcmDefaultStore != "" {
		t.Fatal("linux must resolve libsecret to the secret service and have no windows credential manager")
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
