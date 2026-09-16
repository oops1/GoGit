package credential

import (
	"context"
	"slices"

	"github.com/godbus/dbus/v5"

	"github.com/oops1/gogit/internal/secretservice"
)

type secretServiceBus interface {
	OpenSession(ctx context.Context) (dbus.ObjectPath, error)
	DefaultCollection(ctx context.Context) (dbus.ObjectPath, error)
	SearchItems(ctx context.Context, attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, error)
	CreateItem(ctx context.Context, collection, session dbus.ObjectPath, label string, attrs map[string]string, secret []byte, contentType string) (dbus.ObjectPath, dbus.ObjectPath, error)
	Unlock(ctx context.Context, objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, error)
	Prompt(ctx context.Context, prompt dbus.ObjectPath) (bool, error)
	GetSecret(ctx context.Context, session, item dbus.ObjectPath) ([]byte, error)
	ItemAttributes(ctx context.Context, item dbus.ObjectPath) (map[string]string, error)
	DeleteItem(ctx context.Context, item dbus.ObjectPath) (dbus.ObjectPath, error)
	Close()
}

func dialSecretServiceBus() (secretServiceBus, error) {
	conn, err := secretservice.Dial()
	if err != nil {
		return nil, err
	}
	return conn, nil
}

var dialKeyringBus = dialSecretServiceBus

func openDBusKeyring() (keyring, error) {
	bus, err := dialKeyringBus()
	if err != nil {
		return nil, err
	}
	return &dbusKeyring{bus: bus}, nil
}

type dbusKeyring struct {
	bus     secretServiceBus
	session dbus.ObjectPath
}

func (k *dbusKeyring) close() {
	k.bus.Close()
}

func (k *dbusKeyring) openSession(ctx context.Context) (dbus.ObjectPath, error) {
	if k.session != "" {
		return k.session, nil
	}
	session, err := k.bus.OpenSession(ctx)
	if err != nil {
		return "", err
	}
	k.session = session
	return session, nil
}

func (k *dbusKeyring) runPrompt(ctx context.Context, prompt dbus.ObjectPath) error {
	if secretservice.IsNoObject(prompt) {
		return ErrKeyringLocked
	}
	dismissed, err := k.bus.Prompt(ctx, prompt)
	if err != nil {
		return err
	}
	if dismissed {
		return ErrKeyringLocked
	}
	return nil
}

func (k *dbusKeyring) unlock(ctx context.Context, objects []dbus.ObjectPath) error {
	unlocked, prompt, err := k.bus.Unlock(ctx, objects)
	if err != nil {
		return err
	}
	if len(unlocked) >= len(objects) {
		return nil
	}
	return k.runPrompt(ctx, prompt)
}

func (k *dbusKeyring) search(ctx context.Context, attrs map[string]string) ([]keyringItem, error) {
	session, err := k.openSession(ctx)
	if err != nil {
		return nil, err
	}
	unlocked, locked, err := k.bus.SearchItems(ctx, attrs)
	if err != nil {
		return nil, err
	}
	if len(locked) > 0 {
		if err := k.unlock(ctx, locked); err != nil {
			return nil, err
		}
	}
	paths := slices.Concat(unlocked, locked)
	items := make([]keyringItem, 0, len(paths))
	for _, path := range paths {
		item, err := k.load(ctx, session, path)
		if err != nil {
			wipeKeyringItems(items)
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (k *dbusKeyring) load(ctx context.Context, session, path dbus.ObjectPath) (keyringItem, error) {
	attributes, err := k.bus.ItemAttributes(ctx, path)
	if err != nil {
		return keyringItem{}, err
	}
	secret, err := k.bus.GetSecret(ctx, session, path)
	if err != nil {
		return keyringItem{}, err
	}
	return keyringItem{handle: string(path), attributes: attributes, secret: secret}, nil
}

func (k *dbusKeyring) store(ctx context.Context, label string, attrs map[string]string, secret []byte, contentType string) error {
	session, err := k.openSession(ctx)
	if err != nil {
		return err
	}
	collection, err := k.bus.DefaultCollection(ctx)
	if err != nil {
		return err
	}
	if secretservice.IsNoObject(collection) {
		return ErrNoDefaultKeyring
	}
	if err := k.unlock(ctx, []dbus.ObjectPath{collection}); err != nil {
		return err
	}
	item, prompt, err := k.bus.CreateItem(ctx, collection, session, label, attrs, secret, contentType)
	if err != nil {
		return err
	}
	if secretservice.IsNoObject(item) {
		return k.runPrompt(ctx, prompt)
	}
	return nil
}

func (k *dbusKeyring) remove(ctx context.Context, items []keyringItem) error {
	for _, item := range items {
		prompt, err := k.bus.DeleteItem(ctx, dbus.ObjectPath(item.handle))
		if err != nil {
			return err
		}
		if !secretservice.IsNoObject(prompt) {
			if err := k.runPrompt(ctx, prompt); err != nil {
				return err
			}
		}
	}
	return nil
}
