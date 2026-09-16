package credential

import "context"

const secretSchemaAttribute = "xdg:schema"

type keyringItem struct {
	handle     string
	attributes map[string]string
	secret     []byte
}

type keyring interface {
	search(ctx context.Context, attrs map[string]string) ([]keyringItem, error)
	store(ctx context.Context, label string, attrs map[string]string, secret []byte, contentType string) error
	remove(ctx context.Context, items []keyringItem) error
	close()
}

func wipeKeyringItems(items []keyringItem) {
	for i := range items {
		clear(items[i].secret)
	}
}

func searchKeyring(ctx context.Context, open func() (keyring, error), attrs map[string]string) ([]keyringItem, error) {
	k, err := open()
	if err != nil {
		return nil, err
	}
	defer k.close()
	return k.search(ctx, attrs)
}

type gcmKeyringStore struct {
	open      func() (keyring, error)
	namespace string
}

const (
	gcmSecretSchema      = "com.microsoft.GitCredentialManager"
	gcmSecretContentType = "plain/text"
)

func (s *gcmKeyringStore) fullService(service string) string {
	if s.namespace == "" {
		return service
	}
	return s.namespace + ":" + service
}

func (s *gcmKeyringStore) attributes(service, account string) map[string]string {
	attrs := map[string]string{"service": s.fullService(service)}
	if account != "" {
		attrs["account"] = account
	}
	return attrs
}

func (s *gcmKeyringStore) get(ctx context.Context, service, account string) (Answer, bool, error) {
	items, err := searchKeyring(ctx, s.open, s.attributes(service, account))
	if err != nil || len(items) == 0 {
		return Answer{}, false, err
	}
	defer wipeKeyringItems(items)
	return Answer{Username: items[0].attributes["account"], Password: append([]byte(nil), items[0].secret...)}, true, nil
}

func (s *gcmKeyringStore) put(ctx context.Context, service, account string, secret []byte) error {
	k, err := s.open()
	if err != nil {
		return err
	}
	defer k.close()
	attrs := s.attributes(service, account)
	items, err := k.search(ctx, attrs)
	if err != nil {
		return err
	}
	unchanged := len(items) > 0 && items[0].attributes["account"] == account && secretsEqual(items[0].secret, secret)
	wipeKeyringItems(items)
	if unchanged {
		return nil
	}
	attrs[secretSchemaAttribute] = gcmSecretSchema
	return k.store(ctx, s.fullService(service), attrs, secret, gcmSecretContentType)
}

func (s *gcmKeyringStore) remove(ctx context.Context, service, account string, secret []byte) error {
	k, err := s.open()
	if err != nil {
		return err
	}
	defer k.close()
	items, err := k.search(ctx, s.attributes(service, account))
	if err != nil {
		return err
	}
	defer wipeKeyringItems(items)
	return k.remove(ctx, itemsWithSecret(items, secret))
}

func itemsWithSecret(items []keyringItem, secret []byte) []keyringItem {
	if len(secret) == 0 {
		return items
	}
	var kept []keyringItem
	for _, item := range items {
		if secretsEqual(item.secret, secret) {
			kept = append(kept, item)
		}
	}
	return kept
}
