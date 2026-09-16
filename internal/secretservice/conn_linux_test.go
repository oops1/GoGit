package secretservice

import (
	"context"
	"errors"
	"maps"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const fakeCollectionPath = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")

type fakeItem struct {
	svc *fakeService
	idx int
}

func (it *fakeItem) GetSecret(_ dbus.ObjectPath) (Secret, *dbus.Error) {
	it.svc.mu.Lock()
	defer it.svc.mu.Unlock()
	if it.svc.getSecretErr != nil {
		return Secret{}, it.svc.getSecretErr
	}
	return it.svc.items[it.idx], nil
}

func (it *fakeItem) Delete() (dbus.ObjectPath, *dbus.Error) {
	it.svc.mu.Lock()
	defer it.svc.mu.Unlock()
	if it.svc.deleteErr != nil {
		return "", it.svc.deleteErr
	}
	it.svc.attrs[it.idx] = nil
	return NoObject, nil
}

type fakeItemProperties struct {
	svc *fakeService
	idx int
}

func (p *fakeItemProperties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	p.svc.mu.Lock()
	defer p.svc.mu.Unlock()
	if p.svc.propertyErr != nil {
		return dbus.Variant{}, p.svc.propertyErr
	}
	if p.svc.wrongPropertyType {
		return dbus.MakeVariant(42), nil
	}
	if iface != itemInterface || name != "Attributes" {
		return dbus.Variant{}, dbus.MakeFailedError(errors.New("unknown property"))
	}
	return dbus.MakeVariant(p.svc.attrs[p.idx]), nil
}

type fakePrompt struct {
	conn         *dbus.Conn
	path         dbus.ObjectPath
	dismissed    bool
	silent       bool
	garbageFirst bool
	err          *dbus.Error
}

func (p *fakePrompt) Prompt(_ string) *dbus.Error {
	if p.err != nil {
		return p.err
	}
	if p.silent {
		return nil
	}
	go func() {
		if p.garbageFirst {
			_ = p.conn.Emit(p.path, promptInterface+".Completed", "not a bool", dbus.MakeVariant(""))
		}
		_ = p.conn.Emit(p.path, promptInterface+".Completed", p.dismissed, dbus.MakeVariant(NoObject))
	}()
	return nil
}

func (p *fakePrompt) Dismiss() *dbus.Error {
	return nil
}

type fakeService struct {
	mu                sync.Mutex
	conn              *dbus.Conn
	attrs             []map[string]string
	items             []Secret
	lockedItems       map[int]bool
	openSessionErr    *dbus.Error
	aliasErr          *dbus.Error
	searchErr         *dbus.Error
	createErr         *dbus.Error
	getSecretErr      *dbus.Error
	unlockErr         *dbus.Error
	deleteErr         *dbus.Error
	propertyErr       *dbus.Error
	wrongPropertyType bool
}

func (s *fakeService) OpenSession(_ string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openSessionErr != nil {
		return dbus.Variant{}, "", s.openSessionErr
	}
	return dbus.MakeVariant(""), dbus.ObjectPath("/session/1"), nil
}

func (s *fakeService) ReadAlias(_ string) (dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aliasErr != nil {
		return "", s.aliasErr
	}
	return fakeCollectionPath, nil
}

func (s *fakeService) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.searchErr != nil {
		return nil, nil, s.searchErr
	}
	unlocked := []dbus.ObjectPath{}
	locked := []dbus.ObjectPath{}
	for i, a := range s.attrs {
		if a == nil || !maps.Equal(a, attrs) {
			continue
		}
		if s.lockedItems[i] {
			locked = append(locked, fakeItemPath(i))
		} else {
			unlocked = append(unlocked, fakeItemPath(i))
		}
	}
	return unlocked, locked, nil
}

func (s *fakeService) Unlock(objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unlockErr != nil {
		return nil, "", s.unlockErr
	}
	return objects, NoObject, nil
}

func (s *fakeService) CreateItem(properties map[string]dbus.Variant, secret Secret, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	attrsValue, _ := properties[itemInterface+".Attributes"].Value().(map[string]string)
	s.mu.Lock()
	if s.createErr != nil {
		err := s.createErr
		s.mu.Unlock()
		return "", "", err
	}
	if !replace || secret.ContentType == "" {
		s.mu.Unlock()
		return "", "", dbus.MakeFailedError(errors.New("replace and content type are required"))
	}
	idx := len(s.items)
	s.items = append(s.items, secret)
	s.attrs = append(s.attrs, attrsValue)
	s.mu.Unlock()
	path := fakeItemPath(idx)
	if err := s.conn.Export(&fakeItem{svc: s, idx: idx}, path, itemInterface); err != nil {
		return "", "", dbus.MakeFailedError(err)
	}
	if err := s.conn.Export(&fakeItemProperties{svc: s, idx: idx}, path, "org.freedesktop.DBus.Properties"); err != nil {
		return "", "", dbus.MakeFailedError(err)
	}
	return path, NoObject, nil
}

func (s *fakeService) set(update func(*fakeService)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(s)
}

type malformedOpenSessionService struct{}

func (malformedOpenSessionService) OpenSession(_ string, _ dbus.Variant) (string, *dbus.Error) {
	return "wrong-shape", nil
}

func fakeItemPath(i int) dbus.ObjectPath {
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

func startFakeService(t *testing.T) *fakeService {
	t.Helper()
	conn := ownBusName(t)
	svc := &fakeService{conn: conn}
	if err := conn.Export(svc, servicePath, serviceInterface); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(svc, fakeCollectionPath, "org.freedesktop.Secret.Collection"); err != nil {
		t.Fatal(err)
	}
	return svc
}

func ownBusName(t *testing.T) *dbus.Conn {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		t.Fatal(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("could not own %s: %v", busName, reply)
	}
	return conn
}

func dialTestConn(t *testing.T) *Conn {
	t.Helper()
	c, err := Dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func exportPrompt(t *testing.T, svc *fakeService, prompt *fakePrompt) dbus.ObjectPath {
	t.Helper()
	prompt.conn = svc.conn
	prompt.path = dbus.ObjectPath("/prompt/test")
	if err := svc.conn.Export(prompt, prompt.path, promptInterface); err != nil {
		t.Fatal(err)
	}
	return prompt.path
}

func TestDialFailsWhenAddressUnreachable(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/gogit-secretservice-test.sock")
	if _, err := Dial(); err == nil {
		t.Fatal("expected error when the session bus address is unreachable")
	}
}

func TestIsNoObjectRecognisesEmptyAndRootPaths(t *testing.T) {
	if !IsNoObject("") || !IsNoObject(NoObject) || IsNoObject("/item/1") {
		t.Fatal("IsNoObject must accept only the empty and the root path")
	}
}

func TestConnWireProtocolAgainstFakeService(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeService(t)
	c := dialTestConn(t)
	ctx := context.Background()

	session, err := c.OpenSession(ctx)
	if err != nil || session == "" {
		t.Fatalf("session = %q, err = %v", session, err)
	}
	collection, err := c.DefaultCollection(ctx)
	if err != nil || collection != fakeCollectionPath {
		t.Fatalf("collection = %q, err = %v", collection, err)
	}
	unlocked, prompt, err := c.Unlock(ctx, []dbus.ObjectPath{collection})
	if err != nil || len(unlocked) != 1 || prompt != NoObject {
		t.Fatalf("unlock = %v %q %v", unlocked, prompt, err)
	}
	attrs := map[string]string{"application": "gogit", "id": "abc"}
	item, prompt, err := c.CreateItem(ctx, collection, session, "gogit", attrs, []byte("secretbytes"), "text/plain")
	if err != nil || IsNoObject(item) || prompt != NoObject {
		t.Fatalf("create = %q %q %v", item, prompt, err)
	}
	found, locked, ok, err := c.FindItem(ctx, attrs)
	if err != nil || !ok || locked || found != item {
		t.Fatalf("find = %q %v %v %v", found, locked, ok, err)
	}
	got, err := c.GetSecret(ctx, session, item)
	if err != nil || string(got) != "secretbytes" {
		t.Fatalf("read = %q %v", got, err)
	}
	gotAttrs, err := c.ItemAttributes(ctx, item)
	if err != nil || !maps.Equal(gotAttrs, attrs) {
		t.Fatalf("attributes = %v %v", gotAttrs, err)
	}
	if _, _, ok, err := c.FindItem(ctx, map[string]string{"application": "nope"}); err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
	svc.set(func(s *fakeService) { s.lockedItems = map[int]bool{0: true} })
	if _, locked, ok, err := c.FindItem(ctx, attrs); err != nil || !ok || !locked {
		t.Fatalf("locked find = %v %v %v", locked, ok, err)
	}
	unlockedItems, lockedItems, err := c.SearchItems(ctx, attrs)
	if err != nil || len(unlockedItems) != 0 || len(lockedItems) != 1 {
		t.Fatalf("search = %v %v %v", unlockedItems, lockedItems, err)
	}
	prompt, err = c.DeleteItem(ctx, item)
	if err != nil || prompt != NoObject {
		t.Fatalf("delete = %q %v", prompt, err)
	}
	if _, _, ok, err := c.FindItem(ctx, attrs); err != nil || ok {
		t.Fatalf("deleted item still found: ok=%v err=%v", ok, err)
	}
}

func TestConnCallsFailWhenServerReturnsErrors(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeService(t)
	boom := dbus.NewError("test.Error", []any{"boom"})
	svc.set(func(s *fakeService) {
		s.openSessionErr, s.aliasErr, s.searchErr, s.createErr, s.unlockErr, s.getSecretErr = boom, boom, boom, boom, boom, boom
		s.deleteErr, s.propertyErr = boom, boom
	})
	c := dialTestConn(t)
	ctx := context.Background()
	if _, err := c.OpenSession(ctx); err == nil {
		t.Fatal("OpenSession: expected error")
	}
	if _, err := c.DefaultCollection(ctx); err == nil {
		t.Fatal("DefaultCollection: expected error")
	}
	if _, _, _, err := c.FindItem(ctx, map[string]string{}); err == nil {
		t.Fatal("FindItem: expected error")
	}
	if _, _, err := c.CreateItem(ctx, fakeCollectionPath, "/session/1", "l", map[string]string{}, []byte("s"), "text/plain"); err == nil {
		t.Fatal("CreateItem: expected error")
	}
	if _, _, err := c.Unlock(ctx, []dbus.ObjectPath{fakeCollectionPath}); err == nil {
		t.Fatal("Unlock: expected error")
	}
	svc.set(func(s *fakeService) { s.createErr = nil })
	item, _, err := c.CreateItem(ctx, fakeCollectionPath, "/session/1", "l", map[string]string{}, []byte("s"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetSecret(ctx, "/session/1", item); err == nil {
		t.Fatal("GetSecret: expected error")
	}
	if _, err := c.ItemAttributes(ctx, item); err == nil {
		t.Fatal("ItemAttributes: expected error")
	}
	if _, err := c.DeleteItem(ctx, item); err == nil {
		t.Fatal("DeleteItem: expected error")
	}
	svc.set(func(s *fakeService) { s.propertyErr, s.wrongPropertyType = nil, true })
	if _, err := c.ItemAttributes(ctx, item); err == nil {
		t.Fatal("ItemAttributes: expected error for a property of the wrong type")
	}
}

func TestConnFailsWhenReplyShapeIsWrong(t *testing.T) {
	startPrivateSessionBus(t)
	conn := ownBusName(t)
	if err := conn.Export(malformedOpenSessionService{}, servicePath, serviceInterface); err != nil {
		t.Fatal(err)
	}
	if _, err := dialTestConn(t).OpenSession(context.Background()); err == nil {
		t.Fatal("expected error decoding a malformed reply")
	}
}

func TestConnPromptReportsCompletionAndDismissal(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeService(t)
	c := dialTestConn(t)
	for _, dismissed := range []bool{false, true} {
		path := exportPrompt(t, svc, &fakePrompt{dismissed: dismissed, garbageFirst: true})
		got, err := c.Prompt(context.Background(), path)
		if err != nil || got != dismissed {
			t.Fatalf("prompt = %v, %v; want %v", got, err, dismissed)
		}
	}
}

func TestConnPromptFailsWhenThePromptCallFails(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeService(t)
	path := exportPrompt(t, svc, &fakePrompt{err: dbus.NewError("test.Error", []any{"boom"})})
	if _, err := dialTestConn(t).Prompt(context.Background(), path); err == nil {
		t.Fatal("expected error")
	}
}

func TestConnPromptTimesOut(t *testing.T) {
	startPrivateSessionBus(t)
	svc := startFakeService(t)
	prev := promptTimeout
	promptTimeout = 200 * time.Millisecond
	t.Cleanup(func() { promptTimeout = prev })
	path := exportPrompt(t, svc, &fakePrompt{silent: true})
	if _, err := dialTestConn(t).Prompt(context.Background(), path); !errors.Is(err, ErrPromptTimeout) {
		t.Fatalf("err = %v, want ErrPromptTimeout", err)
	}
}

func TestConnPromptFailsOnAClosedConnection(t *testing.T) {
	startPrivateSessionBus(t)
	startFakeService(t)
	c := dialTestConn(t)
	c.Close()
	if _, err := c.Prompt(context.Background(), "/prompt/none"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPromptCompletionAcceptsOnlyTheAwaitedSignal(t *testing.T) {
	path := dbus.ObjectPath("/prompt/1")
	name := promptInterface + ".Completed"
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
