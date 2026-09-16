package vault

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/godbus/dbus/v5"

	"github.com/oops1/gogit/internal/secretservice"
)

const (
	secretServiceKEKSize     = 32
	secretServiceIDSize      = 16
	secretServiceApplication = "gogit"
	secretServiceContentType = "application/octet-stream"
)

type secretBus interface {
	OpenSession(ctx context.Context) (dbus.ObjectPath, error)
	DefaultCollection(ctx context.Context) (dbus.ObjectPath, error)
	FindItem(ctx context.Context, attrs map[string]string) (item dbus.ObjectPath, locked, found bool, err error)
	CreateItem(ctx context.Context, collection, session dbus.ObjectPath, label string, attrs map[string]string, secret []byte, contentType string) (item, prompt dbus.ObjectPath, err error)
	Unlock(ctx context.Context, objects []dbus.ObjectPath) (unlocked []dbus.ObjectPath, prompt dbus.ObjectPath, err error)
	Prompt(ctx context.Context, prompt dbus.ObjectPath) (dismissed bool, err error)
	GetSecret(ctx context.Context, session, item dbus.ObjectPath) ([]byte, error)
	Close()
}

func dialDBusSecretBus() (secretBus, error) {
	conn, err := secretservice.Dial()
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	return conn, nil
}

var dialSecretBus = dialDBusSecretBus

func SecretServiceAvailable() bool {
	bus, err := dialSecretBus()
	if err != nil {
		return false
	}
	defer bus.Close()
	_, err = bus.OpenSession(context.Background())
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
	if secretservice.IsNoObject(prompt) {
		return ErrKeyringLocked
	}
	dismissed, err := bus.Prompt(ctx, prompt)
	if errors.Is(err, secretservice.ErrPromptTimeout) {
		return fmt.Errorf("%w: %w", ErrKeyringLocked, err)
	}
	if err != nil {
		return err
	}
	if dismissed {
		return ErrKeyringLocked
	}
	return nil
}

func unlockSecretServiceObject(ctx context.Context, bus secretBus, object dbus.ObjectPath) error {
	unlocked, prompt, err := bus.Unlock(ctx, []dbus.ObjectPath{object})
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
	defer bus.Close()

	session, err := bus.OpenSession(ctx)
	if err != nil {
		return Slot{}, err
	}
	collection, err := bus.DefaultCollection(ctx)
	if err != nil {
		return Slot{}, err
	}
	if secretservice.IsNoObject(collection) {
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

	item, prompt, err := bus.CreateItem(ctx, collection, session, u.label, u.attributes(id), kek, secretServiceContentType)
	if err != nil {
		return Slot{}, err
	}
	if secretservice.IsNoObject(item) {
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
	item, locked, found, err := bus.FindItem(ctx, u.attributes(id))
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
	return bus.GetSecret(ctx, session, item)
}

func (u *SecretServiceUnlocker) Unwrap(ctx context.Context, slot Slot) ([]byte, error) {
	if slot.Kind != SlotSecretService {
		return nil, ErrSlotKindMismatch
	}
	bus, err := dialSecretBus()
	if err != nil {
		return nil, ErrSlotUnavailable
	}
	defer bus.Close()

	session, err := bus.OpenSession(ctx)
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
