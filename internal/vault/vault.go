package vault

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

type Credential struct {
	Resource string
	Username string
	Secret   []byte
}

func (c *Credential) Wipe() {
	clear(c.Secret)
	c.Secret = nil
}

type SSHKey struct {
	Host       string
	Path       string
	Passphrase []byte
	Private    []byte
}

func (k *SSHKey) Wipe() {
	clear(k.Passphrase)
	k.Passphrase = nil
	clear(k.Private)
	k.Private = nil
}

type payload struct {
	Credentials []Credential
	SSHKeys     []SSHKey
}

func (p *payload) wipe() {
	for i := range p.Credentials {
		p.Credentials[i].Wipe()
	}
	for i := range p.SSHKeys {
		p.SSHKeys[i].Wipe()
	}
	clear(p.Credentials)
	clear(p.SSHKeys)
	p.Credentials = nil
	p.SSHKeys = nil
}

func cloneCredential(c Credential) Credential {
	return Credential{
		Resource: c.Resource,
		Username: c.Username,
		Secret:   append([]byte(nil), c.Secret...),
	}
}

func cloneSSHKey(k SSHKey) SSHKey {
	return SSHKey{
		Host:       k.Host,
		Path:       k.Path,
		Passphrase: append([]byte(nil), k.Passphrase...),
		Private:    append([]byte(nil), k.Private...),
	}
}

func clonePayload(p *payload) *payload {
	out := &payload{
		Credentials: make([]Credential, len(p.Credentials)),
		SSHKeys:     make([]SSHKey, len(p.SSHKeys)),
	}
	for i, c := range p.Credentials {
		out.Credentials[i] = cloneCredential(c)
	}
	for i, k := range p.SSHKeys {
		out.SSHKeys[i] = cloneSSHKey(k)
	}
	return out
}

func prefixCandidates(resource string) []string {
	parts := strings.Split(resource, "/")
	out := make([]string, 0, len(parts))
	for i := len(parts); i > 0; i-- {
		out = append(out, strings.Join(parts[:i], "/"))
	}
	return out
}

type Vault struct {
	mu         sync.Mutex
	opts       Options
	version    uint16
	flags      uint16
	slots      []Slot
	nonce      []byte
	ciphertext []byte
	dek        []byte
	secrets    *payload
	timer      *time.Timer
	closed     bool
}

func Open(opts Options) (*Vault, error) {
	if opts.Path == "" {
		return nil, ErrInvalidPath
	}
	version, flags, slots, nonce, ciphertext, err := readVaultFile(opts.Path)
	if err != nil {
		return nil, err
	}
	return &Vault{
		opts:       opts,
		version:    version,
		flags:      flags,
		slots:      slots,
		nonce:      nonce,
		ciphertext: ciphertext,
	}, nil
}

func Create(ctx context.Context, opts Options, first Unlocker) (*Vault, error) {
	if opts.Path == "" {
		return nil, ErrInvalidPath
	}
	if first == nil {
		return nil, ErrNilUnlocker
	}
	if _, err := os.Stat(opts.Path); err == nil {
		return nil, ErrAlreadyExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("vault: %w", err)
	}
	dek := make([]byte, chacha20poly1305.KeySize)
	_, _ = rand.Read(dek)
	slot, err := first.Wrap(ctx, dek)
	if err != nil {
		return nil, err
	}
	v := &Vault{
		opts:    opts,
		version: formatVersion,
		flags:   0,
		dek:     dek,
		secrets: &payload{},
	}
	if err := v.persistWithLocked([]Slot{slot}, v.secrets); err != nil {
		return nil, err
	}
	v.touchLocked()
	return v, nil
}

func (v *Vault) Path() string {
	return v.opts.Path
}

func (v *Vault) Exists() bool {
	_, err := os.Stat(v.opts.Path)
	return err == nil
}

func (v *Vault) Slots() []SlotInfo {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]SlotInfo, len(v.slots))
	for i, s := range v.slots {
		out[i] = SlotInfo{Kind: s.Kind, Index: i}
	}
	return out
}

func (v *Vault) Locked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dek == nil
}

func (v *Vault) Unlock(ctx context.Context, unlocker Unlocker) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if unlocker == nil {
		return ErrNilUnlocker
	}
	kind := unlocker.Kind()
	var lastErr error
	for _, slot := range v.slots {
		if slot.Kind != kind {
			continue
		}
		dek, err := unlocker.Unwrap(ctx, slot)
		if err != nil {
			lastErr = err
			continue
		}
		secrets, err := v.decryptPayloadLocked(dek)
		if err != nil {
			clear(dek)
			return err
		}
		v.dek = dek
		v.secrets = secrets
		v.touchLocked()
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return ErrSlotNotFound
}

func (v *Vault) decryptPayloadLocked(dek []byte) (*payload, error) {
	headerBytes, err := encodeHeader(v.version, v.flags, v.slots)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(dek)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	plaintext, err := aead.Open(nil, v.nonce, v.ciphertext, headerBytes)
	if err != nil {
		return nil, ErrCorrupted
	}
	defer clear(plaintext)
	var p payload
	if err := json.Unmarshal(plaintext, &p); err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	return &p, nil
}

func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lockLocked()
}

func (v *Vault) lockLocked() {
	if v.dek != nil {
		clear(v.dek)
		v.dek = nil
	}
	if v.secrets != nil {
		v.secrets.wipe()
		v.secrets = nil
	}
	if v.timer != nil {
		v.timer.Stop()
	}
}

func (v *Vault) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lockLocked()
	v.closed = true
	return nil
}

func (v *Vault) AddSlot(ctx context.Context, unlocker Unlocker) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if unlocker == nil {
		return ErrNilUnlocker
	}
	if v.dek == nil {
		return ErrLocked
	}
	if len(v.slots) >= maxSlotCount {
		return ErrTooManySlots
	}
	slot, err := unlocker.Wrap(ctx, v.dek)
	if err != nil {
		return err
	}
	nextSlots := append(slices.Clone(v.slots), slot)
	if err := v.persistWithLocked(nextSlots, v.secrets); err != nil {
		return err
	}
	v.touchLocked()
	return nil
}

func (v *Vault) RemoveSlot(index int) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	if index < 0 || index >= len(v.slots) {
		return ErrSlotIndex
	}
	if len(v.slots) <= minSlotCount {
		return ErrLastSlot
	}
	nextSlots := slices.Delete(slices.Clone(v.slots), index, index+1)
	if err := v.persistWithLocked(nextSlots, v.secrets); err != nil {
		return err
	}
	v.touchLocked()
	return nil
}

func (v *Vault) persistWithLocked(slots []Slot, p *payload) error {
	headerBytes, err := encodeHeader(v.version, v.flags, slots)
	if err != nil {
		return err
	}
	plaintext, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	defer clear(plaintext)
	nonce := make([]byte, payloadNonceSize)
	_, _ = rand.Read(nonce)
	aead, err := chacha20poly1305.NewX(v.dek)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, headerBytes)
	data := make([]byte, 0, len(headerBytes)+len(nonce)+len(ciphertext))
	data = append(data, headerBytes...)
	data = append(data, nonce...)
	data = append(data, ciphertext...)
	if err := writeAtomic(v.opts.Path, data); err != nil {
		return err
	}
	v.slots = slots
	v.nonce = nonce
	v.ciphertext = ciphertext
	v.secrets = p
	return nil
}

func (v *Vault) touchLocked() {
	if v.opts.IdleTime <= 0 {
		return
	}
	if v.timer == nil {
		v.timer = time.AfterFunc(v.opts.IdleTime, v.onIdle)
		return
	}
	v.timer.Reset(v.opts.IdleTime)
}

func (v *Vault) onIdle() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dek == nil {
		return
	}
	v.lockLocked()
}

func (v *Vault) Credential(resource string) (Credential, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dek == nil {
		return Credential{}, false
	}
	v.touchLocked()
	for _, candidate := range prefixCandidates(resource) {
		for _, c := range v.secrets.Credentials {
			if c.Resource == candidate {
				return cloneCredential(c), true
			}
		}
	}
	return Credential{}, false
}

func (v *Vault) SetCredential(c Credential) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	next := clonePayload(v.secrets)
	stored := cloneCredential(c)
	idx := -1
	for i, existing := range next.Credentials {
		if existing.Resource == c.Resource {
			idx = i
			break
		}
	}
	if idx >= 0 {
		next.Credentials[idx].Wipe()
		next.Credentials[idx] = stored
	} else {
		next.Credentials = append(next.Credentials, stored)
	}
	if err := v.persistWithLocked(v.slots, next); err != nil {
		return err
	}
	v.touchLocked()
	return nil
}

func (v *Vault) DeleteCredential(resource string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	idx := -1
	for i, c := range v.secrets.Credentials {
		if c.Resource == resource {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	next := clonePayload(v.secrets)
	next.Credentials[idx].Wipe()
	next.Credentials = slices.Delete(next.Credentials, idx, idx+1)
	if err := v.persistWithLocked(v.slots, next); err != nil {
		return err
	}
	v.touchLocked()
	return nil
}

func (v *Vault) Resources() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dek == nil {
		return nil
	}
	v.touchLocked()
	out := make([]string, len(v.secrets.Credentials))
	for i, c := range v.secrets.Credentials {
		out[i] = c.Resource
	}
	slices.Sort(out)
	return out
}

func (v *Vault) SSHKey(host string) (SSHKey, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dek == nil {
		return SSHKey{}, false
	}
	v.touchLocked()
	for _, k := range v.secrets.SSHKeys {
		if k.Host == host {
			return cloneSSHKey(k), true
		}
	}
	for _, k := range v.secrets.SSHKeys {
		if k.Host == "*" {
			return cloneSSHKey(k), true
		}
	}
	return SSHKey{}, false
}

func (v *Vault) SetSSHKey(k SSHKey) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	next := clonePayload(v.secrets)
	stored := cloneSSHKey(k)
	idx := -1
	for i, existing := range next.SSHKeys {
		if existing.Host == k.Host {
			idx = i
			break
		}
	}
	if idx >= 0 {
		next.SSHKeys[idx].Wipe()
		next.SSHKeys[idx] = stored
	} else {
		next.SSHKeys = append(next.SSHKeys, stored)
	}
	if err := v.persistWithLocked(v.slots, next); err != nil {
		return err
	}
	v.touchLocked()
	return nil
}

func (v *Vault) DeleteSSHKey(host string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	idx := -1
	for i, k := range v.secrets.SSHKeys {
		if k.Host == host {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	next := clonePayload(v.secrets)
	next.SSHKeys[idx].Wipe()
	next.SSHKeys = slices.Delete(next.SSHKeys, idx, idx+1)
	if err := v.persistWithLocked(v.slots, next); err != nil {
		return err
	}
	v.touchLocked()
	return nil
}

func (v *Vault) Hosts() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dek == nil {
		return nil
	}
	v.touchLocked()
	out := make([]string, len(v.secrets.SSHKeys))
	for i, k := range v.secrets.SSHKeys {
		out[i] = k.Host
	}
	slices.Sort(out)
	return out
}
