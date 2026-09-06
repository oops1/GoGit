package vault

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceKEKSize      = 32
	secretServiceIDSize       = 16
	secretServiceBusName      = "org.freedesktop.secrets"
	secretServiceObjectPath   = dbus.ObjectPath("/org/freedesktop/secrets")
	secretServiceDefaultAlias = dbus.ObjectPath("/org/freedesktop/secrets/aliases/default")
	secretServiceApplication  = "gogit"
)

type secretServiceSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type secretBus interface {
	openSession() (dbus.ObjectPath, error)
	findItem(attrs map[string]string) (dbus.ObjectPath, bool, error)
	createItem(session dbus.ObjectPath, label string, attrs map[string]string, secret []byte) error
	readSecret(session, item dbus.ObjectPath) ([]byte, error)
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

func (b *dbusSecretBus) service() dbus.BusObject {
	return b.conn.Object(secretServiceBusName, secretServiceObjectPath)
}

func (b *dbusSecretBus) openSession() (dbus.ObjectPath, error) {
	var (
		output  dbus.Variant
		session dbus.ObjectPath
	)
	call := b.service().Call("org.freedesktop.Secret.Service.OpenSession", 0, "plain", dbus.MakeVariant(""))
	if call.Err != nil {
		return "", fmt.Errorf("vault: %w", call.Err)
	}
	if err := call.Store(&output, &session); err != nil {
		return "", fmt.Errorf("vault: %w", err)
	}
	return session, nil
}

func (b *dbusSecretBus) findItem(attrs map[string]string) (dbus.ObjectPath, bool, error) {
	var (
		unlocked []dbus.ObjectPath
		locked   []dbus.ObjectPath
	)
	call := b.service().Call("org.freedesktop.Secret.Service.SearchItems", 0, attrs)
	if call.Err != nil {
		return "", false, fmt.Errorf("vault: %w", call.Err)
	}
	if err := call.Store(&unlocked, &locked); err != nil {
		return "", false, fmt.Errorf("vault: %w", err)
	}
	if len(unlocked) == 0 {
		return "", false, nil
	}
	return unlocked[0], true, nil
}

func (b *dbusSecretBus) createItem(session dbus.ObjectPath, label string, attrs map[string]string, secret []byte) error {
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
	collection := b.conn.Object(secretServiceBusName, secretServiceDefaultAlias)
	call := collection.Call("org.freedesktop.Secret.Collection.CreateItem", 0, properties, value, true)
	if call.Err != nil {
		return fmt.Errorf("vault: %w", call.Err)
	}
	return nil
}

func (b *dbusSecretBus) readSecret(session, item dbus.ObjectPath) ([]byte, error) {
	itemObject := b.conn.Object(secretServiceBusName, item)
	var secret secretServiceSecret
	call := itemObject.Call("org.freedesktop.Secret.Item.GetSecret", 0, session)
	if call.Err != nil {
		return nil, fmt.Errorf("vault: %w", call.Err)
	}
	if err := call.Store(&secret); err != nil {
		return nil, fmt.Errorf("vault: %w", err)
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
	_, err = bus.openSession()
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

func (u *SecretServiceUnlocker) Wrap(_ context.Context, dek []byte) (Slot, error) {
	bus, err := dialSecretBus()
	if err != nil {
		return Slot{}, ErrSlotUnavailable
	}
	defer bus.close()

	id := make([]byte, secretServiceIDSize)
	_, _ = rand.Read(id)
	kek := make([]byte, secretServiceKEKSize)
	_, _ = rand.Read(kek)
	defer clear(kek)

	session, err := bus.openSession()
	if err != nil {
		return Slot{}, err
	}
	if err := bus.createItem(session, u.label, u.attributes(id), kek); err != nil {
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

func (u *SecretServiceUnlocker) Unwrap(_ context.Context, slot Slot) ([]byte, error) {
	if slot.Kind != SlotSecretService {
		return nil, ErrSlotKindMismatch
	}
	bus, err := dialSecretBus()
	if err != nil {
		return nil, ErrSlotUnavailable
	}
	defer bus.close()

	session, err := bus.openSession()
	if err != nil {
		return nil, err
	}
	item, ok, err := bus.findItem(u.attributes(slot.Salt))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrWrongKey
	}
	kek, err := bus.readSecret(session, item)
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
