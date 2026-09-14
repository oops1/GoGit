package vault

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceKEKSize      = 32
	secretServiceIDSize       = 16
	secretServiceBusName      = "org.freedesktop.secrets"
	secretServiceObjectPath   = dbus.ObjectPath("/org/freedesktop/secrets")
	secretServiceNoObject     = dbus.ObjectPath("/")
	secretServiceApplication  = "gogit"
	secretServiceInterface    = "org.freedesktop.Secret.Service"
	secretServicePrompt       = "org.freedesktop.Secret.Prompt"
	secretServiceDefaultAlias = "default"
)

var (
	secretServiceCallTimeout   = 10 * time.Second
	secretServicePromptTimeout = 2 * time.Minute
)

type secretServiceSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type secretBus interface {
	openSession(ctx context.Context) (dbus.ObjectPath, error)
	defaultCollection(ctx context.Context) (dbus.ObjectPath, error)
	findItem(ctx context.Context, attrs map[string]string) (item dbus.ObjectPath, locked, found bool, err error)
	createItem(ctx context.Context, collection, session dbus.ObjectPath, label string, attrs map[string]string, secret []byte) (item, prompt dbus.ObjectPath, err error)
	unlock(ctx context.Context, objects []dbus.ObjectPath) (unlocked []dbus.ObjectPath, prompt dbus.ObjectPath, err error)
	prompt(ctx context.Context, prompt dbus.ObjectPath) (dismissed bool, err error)
	readSecret(ctx context.Context, session, item dbus.ObjectPath) ([]byte, error)
	close()
}

type dbusSecretBus struct {
	conn *dbus.Conn
}

func newDBusSecretBus() (secretBus, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	return &dbusSecretBus{conn: conn}, nil
}

func (b *dbusSecretBus) close() {
	_ = b.conn.Close()
}

func (b *dbusSecretBus) call(ctx context.Context, object dbus.ObjectPath, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(ctx, secretServiceCallTimeout)
	defer cancel()
	return b.conn.Object(secretServiceBusName, object).CallWithContext(ctx, method, 0, args...)
}

func stored(call *dbus.Call, into ...any) error {
	if call.Err != nil {
		return fmt.Errorf("vault: %w", call.Err)
	}
	if err := call.Store(into...); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	return nil
}

func (b *dbusSecretBus) openSession(ctx context.Context) (dbus.ObjectPath, error) {
	var (
		output  dbus.Variant
		session dbus.ObjectPath
	)
	call := b.call(ctx, secretServiceObjectPath, secretServiceInterface+".OpenSession", "plain", dbus.MakeVariant(""))
	if err := stored(call, &output, &session); err != nil {
		return "", err
	}
	return session, nil
}

func (b *dbusSecretBus) defaultCollection(ctx context.Context) (dbus.ObjectPath, error) {
	var collection dbus.ObjectPath
	call := b.call(ctx, secretServiceObjectPath, secretServiceInterface+".ReadAlias", secretServiceDefaultAlias)
	if err := stored(call, &collection); err != nil {
		return "", err
	}
	return collection, nil
}

func (b *dbusSecretBus) findItem(ctx context.Context, attrs map[string]string) (dbus.ObjectPath, bool, bool, error) {
	var unlocked, locked []dbus.ObjectPath
	call := b.call(ctx, secretServiceObjectPath, secretServiceInterface+".SearchItems", attrs)
	if err := stored(call, &unlocked, &locked); err != nil {
		return "", false, false, err
	}
	switch {
	case len(unlocked) > 0:
		return unlocked[0], false, true, nil
	case len(locked) > 0:
		return locked[0], true, true, nil
	default:
		return "", false, false, nil
	}
}

func (b *dbusSecretBus) createItem(ctx context.Context, collection, session dbus.ObjectPath, label string, attrs map[string]string, secret []byte) (dbus.ObjectPath, dbus.ObjectPath, error) {
	properties := map[string]dbus.Variant{
		"org.freedesktop.Secret.Item.Label":      dbus.MakeVariant(label),
		"org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(attrs),
	}
	value := secretServiceSecret{
		Session:     session,
		Parameters:  []byte{},
		Value:       secret,
		ContentType: "application/octet-stream",
	}
	var item, prompt dbus.ObjectPath
	call := b.call(ctx, collection, "org.freedesktop.Secret.Collection.CreateItem", properties, value, true)
	if err := stored(call, &item, &prompt); err != nil {
		return "", "", err
	}
	return item, prompt, nil
}

func (b *dbusSecretBus) unlock(ctx context.Context, objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, error) {
	var (
		unlocked []dbus.ObjectPath
		prompt   dbus.ObjectPath
	)
	call := b.call(ctx, secretServiceObjectPath, secretServiceInterface+".Unlock", objects)
	if err := stored(call, &unlocked, &prompt); err != nil {
		return nil, "", err
	}
	return unlocked, prompt, nil
}

func (b *dbusSecretBus) prompt(ctx context.Context, prompt dbus.ObjectPath) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, secretServicePromptTimeout)
	defer cancel()
	match := []dbus.MatchOption{
		dbus.WithMatchObjectPath(prompt),
		dbus.WithMatchInterface(secretServicePrompt),
		dbus.WithMatchMember("Completed"),
	}
	if err := b.conn.AddMatchSignalContext(ctx, match...); err != nil {
		return false, fmt.Errorf("vault: %w", err)
	}
	defer func() { _ = b.conn.RemoveMatchSignal(match...) }()
	signals := make(chan *dbus.Signal, 8)
	b.conn.Signal(signals)
	defer b.conn.RemoveSignal(signals)
	if call := b.call(ctx, prompt, secretServicePrompt+".Prompt", ""); call.Err != nil {
		return false, fmt.Errorf("vault: %w", call.Err)
	}
	for {
		select {
		case signal := <-signals:
			if dismissed, ok := promptCompletion(signal, prompt); ok {
				return dismissed, nil
			}
		case <-ctx.Done():
			b.conn.Object(secretServiceBusName, prompt).Go(secretServicePrompt+".Dismiss", dbus.FlagNoReplyExpected, nil)
			return false, fmt.Errorf("%w: %w", ErrKeyringLocked, ctx.Err())
		}
	}
}

func promptCompletion(signal *dbus.Signal, prompt dbus.ObjectPath) (dismissed, ok bool) {
	if signal == nil || signal.Path != prompt || signal.Name != secretServicePrompt+".Completed" || len(signal.Body) == 0 {
		return false, false
	}
	dismissed, ok = signal.Body[0].(bool)
	return dismissed, ok
}

func (b *dbusSecretBus) readSecret(ctx context.Context, session, item dbus.ObjectPath) ([]byte, error) {
	var secret secretServiceSecret
	call := b.call(ctx, item, "org.freedesktop.Secret.Item.GetSecret", session)
	if err := stored(call, &secret); err != nil {
		return nil, err
	}
	return secret.Value, nil
}

var dialSecretBus = newDBusSecretBus

func SecretServiceAvailable() bool {
	bus, err := dialSecretBus()
	if err != nil {
		return false
	}
	defer bus.close()
	_, err = bus.openSession(context.Background())
	return err == nil
}

type SecretServiceUnlocker struct {
	label string
}

func NewSecretServiceUnlocker(label string) *SecretServiceUnlocker {
	return &SecretServiceUnlocker{label: label}
}

func (u *SecretServiceUnlocker) Kind() SlotKind {
	return SlotSecretService
}

func (u *SecretServiceUnlocker) attributes(id []byte) map[string]string {
	return map[string]string{
		"application": secretServiceApplication,
		"label":       u.label,
		"id":          hex.EncodeToString(id),
	}
}

func runSecretServicePrompt(ctx context.Context, bus secretBus, prompt dbus.ObjectPath) error {
	if prompt == "" || prompt == secretServiceNoObject {
		return ErrKeyringLocked
	}
	dismissed, err := bus.prompt(ctx, prompt)
	if err != nil {
		return err
	}
	if dismissed {
		return ErrKeyringLocked
	}
	return nil
}

func unlockSecretServiceObject(ctx context.Context, bus secretBus, object dbus.ObjectPath) error {
	unlocked, prompt, err := bus.unlock(ctx, []dbus.ObjectPath{object})
	if err != nil {
		return err
	}
	if slices.Contains(unlocked, object) {
		return nil
	}
	return runSecretServicePrompt(ctx, bus, prompt)
}

func (u *SecretServiceUnlocker) Wrap(ctx context.Context, dek []byte) (Slot, error) {
	bus, err := dialSecretBus()
	if err != nil {
		return Slot{}, ErrSlotUnavailable
	}
	defer bus.close()

	session, err := bus.openSession(ctx)
	if err != nil {
		return Slot{}, err
	}
	collection, err := bus.defaultCollection(ctx)
	if err != nil {
		return Slot{}, err
	}
	if collection == "" || collection == secretServiceNoObject {
		return Slot{}, ErrSlotUnavailable
	}
	if err := unlockSecretServiceObject(ctx, bus, collection); err != nil {
		return Slot{}, err
	}

	id := make([]byte, secretServiceIDSize)
	_, _ = rand.Read(id)
	kek := make([]byte, secretServiceKEKSize)
	_, _ = rand.Read(kek)
	defer clear(kek)

	item, prompt, err := bus.createItem(ctx, collection, session, u.label, u.attributes(id), kek)
	if err != nil {
		return Slot{}, err
	}
	if item == "" || item == secretServiceNoObject {
		if err := runSecretServicePrompt(ctx, bus, prompt); err != nil {
			return Slot{}, err
		}
	}
	if err := u.verifyStoredKey(ctx, bus, session, id, kek); err != nil {
		return Slot{}, err
	}

	nonce := make([]byte, slotNonceSize)
	_, _ = rand.Read(nonce)
	aead, err := newXChaCha20Poly1305(kek)
	if err != nil {
		return Slot{}, fmt.Errorf("vault: %w", err)
	}
	wrapped := aead.Seal(nil, nonce, dek, slotAAD(SlotSecretService, id))
	return Slot{
		Kind:    SlotSecretService,
		Salt:    id,
		Nonce:   nonce,
		Wrapped: wrapped,
	}, nil
}

func (u *SecretServiceUnlocker) verifyStoredKey(ctx context.Context, bus secretBus, session dbus.ObjectPath, id, kek []byte) error {
	got, err := u.readKey(ctx, bus, session, id)
	if errors.Is(err, ErrWrongKey) {
		return ErrKeyNotStored
	}
	if err != nil {
		return err
	}
	defer clear(got)
	if subtle.ConstantTimeCompare(got, kek) != 1 {
		return ErrKeyNotStored
	}
	return nil
}

func (u *SecretServiceUnlocker) readKey(ctx context.Context, bus secretBus, session dbus.ObjectPath, id []byte) ([]byte, error) {
	item, locked, found, err := bus.findItem(ctx, u.attributes(id))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrWrongKey
	}
	if locked {
		if err := unlockSecretServiceObject(ctx, bus, item); err != nil {
			return nil, err
		}
	}
	return bus.readSecret(ctx, session, item)
}

func (u *SecretServiceUnlocker) Unwrap(ctx context.Context, slot Slot) ([]byte, error) {
	if slot.Kind != SlotSecretService {
		return nil, ErrSlotKindMismatch
	}
	bus, err := dialSecretBus()
	if err != nil {
		return nil, ErrSlotUnavailable
	}
	defer bus.close()

	session, err := bus.openSession(ctx)
	if err != nil {
		return nil, err
	}
	kek, err := u.readKey(ctx, bus, session, slot.Salt)
	if err != nil {
		return nil, err
	}
	defer clear(kek)

	aead, err := newXChaCha20Poly1305(kek)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	dek, err := aead.Open(nil, slot.Nonce, slot.Wrapped, slotAAD(SlotSecretService, slot.Salt))
	if err != nil {
		return nil, ErrWrongKey
	}
	return dek, nil
}
