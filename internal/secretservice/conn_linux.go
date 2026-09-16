package secretservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	busName          = "org.freedesktop.secrets"
	servicePath      = dbus.ObjectPath("/org/freedesktop/secrets")
	serviceInterface = "org.freedesktop.Secret.Service"
	promptInterface  = "org.freedesktop.Secret.Prompt"
	itemInterface    = "org.freedesktop.Secret.Item"
	collectionMethod = "org.freedesktop.Secret.Collection.CreateItem"
	defaultAlias     = "default"
	NoObject         = dbus.ObjectPath("/")
)

var (
	callTimeout   = 10 * time.Second
	promptTimeout = 2 * time.Minute
)

var ErrPromptTimeout = errors.New("secretservice: the keyring prompt was not answered in time")

type Secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type Conn struct {
	conn *dbus.Conn
}

func Dial() (*Conn, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("secretservice: %w", err)
	}
	return &Conn{conn: conn}, nil
}

func IsNoObject(path dbus.ObjectPath) bool {
	return path == "" || path == NoObject
}

func (c *Conn) Close() {
	_ = c.conn.Close()
}

func (c *Conn) call(ctx context.Context, object dbus.ObjectPath, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return c.conn.Object(busName, object).CallWithContext(ctx, method, 0, args...)
}

func stored(call *dbus.Call, into ...any) error {
	if call.Err != nil {
		return fmt.Errorf("secretservice: %w", call.Err)
	}
	if err := call.Store(into...); err != nil {
		return fmt.Errorf("secretservice: %w", err)
	}
	return nil
}

func (c *Conn) OpenSession(ctx context.Context) (dbus.ObjectPath, error) {
	var (
		output  dbus.Variant
		session dbus.ObjectPath
	)
	call := c.call(ctx, servicePath, serviceInterface+".OpenSession", "plain", dbus.MakeVariant(""))
	if err := stored(call, &output, &session); err != nil {
		return "", err
	}
	return session, nil
}

func (c *Conn) DefaultCollection(ctx context.Context) (dbus.ObjectPath, error) {
	var collection dbus.ObjectPath
	call := c.call(ctx, servicePath, serviceInterface+".ReadAlias", defaultAlias)
	if err := stored(call, &collection); err != nil {
		return "", err
	}
	return collection, nil
}

func (c *Conn) SearchItems(ctx context.Context, attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	call := c.call(ctx, servicePath, serviceInterface+".SearchItems", attrs)
	if err := stored(call, &unlocked, &locked); err != nil {
		return nil, nil, err
	}
	return unlocked, locked, nil
}

func (c *Conn) FindItem(ctx context.Context, attrs map[string]string) (dbus.ObjectPath, bool, bool, error) {
	unlocked, locked, err := c.SearchItems(ctx, attrs)
	if err != nil {
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

func (c *Conn) CreateItem(ctx context.Context, collection, session dbus.ObjectPath, label string, attrs map[string]string, secret []byte, contentType string) (dbus.ObjectPath, dbus.ObjectPath, error) {
	properties := map[string]dbus.Variant{
		itemInterface + ".Label":      dbus.MakeVariant(label),
		itemInterface + ".Attributes": dbus.MakeVariant(attrs),
	}
	value := Secret{
		Session:     session,
		Parameters:  []byte{},
		Value:       secret,
		ContentType: contentType,
	}
	var item, prompt dbus.ObjectPath
	call := c.call(ctx, collection, collectionMethod, properties, value, true)
	if err := stored(call, &item, &prompt); err != nil {
		return "", "", err
	}
	return item, prompt, nil
}

func (c *Conn) Unlock(ctx context.Context, objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, error) {
	var (
		unlocked []dbus.ObjectPath
		prompt   dbus.ObjectPath
	)
	call := c.call(ctx, servicePath, serviceInterface+".Unlock", objects)
	if err := stored(call, &unlocked, &prompt); err != nil {
		return nil, "", err
	}
	return unlocked, prompt, nil
}

func (c *Conn) Prompt(ctx context.Context, prompt dbus.ObjectPath) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()
	match := []dbus.MatchOption{
		dbus.WithMatchObjectPath(prompt),
		dbus.WithMatchInterface(promptInterface),
		dbus.WithMatchMember("Completed"),
	}
	if err := c.conn.AddMatchSignalContext(ctx, match...); err != nil {
		return false, fmt.Errorf("secretservice: %w", err)
	}
	defer func() { _ = c.conn.RemoveMatchSignal(match...) }()
	signals := make(chan *dbus.Signal, 8)
	c.conn.Signal(signals)
	defer c.conn.RemoveSignal(signals)
	if call := c.call(ctx, prompt, promptInterface+".Prompt", ""); call.Err != nil {
		return false, fmt.Errorf("secretservice: %w", call.Err)
	}
	for {
		select {
		case signal := <-signals:
			if dismissed, ok := promptCompletion(signal, prompt); ok {
				return dismissed, nil
			}
		case <-ctx.Done():
			c.conn.Object(busName, prompt).Go(promptInterface+".Dismiss", dbus.FlagNoReplyExpected, nil)
			return false, fmt.Errorf("%w: %w", ErrPromptTimeout, ctx.Err())
		}
	}
}

func promptCompletion(signal *dbus.Signal, prompt dbus.ObjectPath) (dismissed, ok bool) {
	if signal == nil || signal.Path != prompt || signal.Name != promptInterface+".Completed" || len(signal.Body) == 0 {
		return false, false
	}
	dismissed, ok = signal.Body[0].(bool)
	return dismissed, ok
}

func (c *Conn) GetSecret(ctx context.Context, session, item dbus.ObjectPath) ([]byte, error) {
	var secret Secret
	call := c.call(ctx, item, itemInterface+".GetSecret", session)
	if err := stored(call, &secret); err != nil {
		return nil, err
	}
	return secret.Value, nil
}

func (c *Conn) ItemAttributes(ctx context.Context, item dbus.ObjectPath) (map[string]string, error) {
	var attrs map[string]string
	call := c.call(ctx, item, "org.freedesktop.DBus.Properties.Get", itemInterface, "Attributes")
	var value dbus.Variant
	if err := stored(call, &value); err != nil {
		return nil, err
	}
	if err := value.Store(&attrs); err != nil {
		return nil, fmt.Errorf("secretservice: %w", err)
	}
	return attrs, nil
}

func (c *Conn) DeleteItem(ctx context.Context, item dbus.ObjectPath) (dbus.ObjectPath, error) {
	var prompt dbus.ObjectPath
	call := c.call(ctx, item, itemInterface+".Delete")
	if err := stored(call, &prompt); err != nil {
		return "", err
	}
	return prompt, nil
}
