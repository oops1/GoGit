package vault

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/oops1/gogit/internal/safefile"
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
	shortest := 1
	if idx := strings.Index(resource, "://"); idx > 0 && !strings.Contains(resource[:idx], "/") {
		shortest = 3
	}
	out := make([]string, 0, len(parts))
	for i := len(parts); i >= shortest; i-- {
		out = append(out, strings.Join(parts[:i], "/"))
	}
	return out
}

type Vault struct {
	mu      sync.Mutex
	opts    Options
	file    vaultFile
	highest uint64
	dek     []byte
	secrets *payload
	timer   *time.Timer
	closed  bool
}

type revision struct {
	slots   []Slot
	dek     []byte
	secrets *payload
}

func Open(opts Options) (*Vault, error) {
	if opts.Path == "" {
		return nil, ErrInvalidPath
	}
	data, err := safefile.Read(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	file, err := parseVaultFile(data)
	if err != nil {
		return nil, err
	}
	if file.header.generation < opts.KnownGeneration {
		return nil, ErrRollback
	}
	return &Vault{opts: opts, file: file, highest: file.header.generation}, nil
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
	}
	dek := make([]byte, chacha20poly1305.KeySize)
	_, _ = rand.Read(dek)
	slot, err := first.Wrap(ctx, dek)
	if err != nil {
		clear(dek)
		return nil, err
	}
	v := &Vault{opts: opts, highest: opts.KnownGeneration}
	rev := revision{slots: []Slot{slot}, dek: dek, secrets: &payload{}}
	var written vaultFile
	err = safefile.Update(opts.Path, func(current []byte) ([]byte, error) {
		if current != nil {
			return nil, ErrAlreadyExists
		}
		data, file, err := v.seal(rev)
		written = file
		return data, err
	})
	if err != nil {
		clear(dek)
		return nil, err
	}
	v.adoptLocked(written, rev)
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

func (v *Vault) Generation() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.highest
}

func (v *Vault) Slots() []SlotInfo {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]SlotInfo, len(v.file.header.slots))
	for i, s := range v.file.header.slots {
		out[i] = SlotInfo{Kind: s.Kind, Index: i}
	}
	return out
}

func (v *Vault) HasSlot(kind SlotKind) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.ContainsFunc(v.file.header.slots, func(s Slot) bool { return s.Kind == kind })
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
	if err := v.refreshLocked(); err != nil {
		return err
	}
	kind := unlocker.Kind()
	var lastErr error
	for _, slot := range v.file.header.slots {
		if slot.Kind != kind {
			continue
		}
		dek, err := unlocker.Unwrap(ctx, slot)
		if err != nil {
			lastErr = err
			continue
		}
		secrets, err := openPayload(v.file, dek)
		if err != nil {
			clear(dek)
			return err
		}
		v.adoptLocked(v.file, revision{dek: dek, secrets: secrets})
		v.touchLocked()
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return ErrSlotNotFound
}

func openPayload(file vaultFile, dek []byte) (*payload, error) {
	aead, err := newXChaCha20Poly1305(dek)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	plaintext, err := aead.Open(nil, file.nonce, file.ciphertext, file.headerRaw)
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

func (v *Vault) refreshLocked() error {
	data, err := safefile.Read(v.opts.Path)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	file, changed, err := v.changedFileLocked(data)
	if err != nil || !changed {
		return err
	}
	v.lockLocked()
	v.file = file
	v.noteGenerationLocked(file.header.generation)
	return nil
}

func (v *Vault) changedFileLocked(data []byte) (vaultFile, bool, error) {
	if sha256.Sum256(data) == v.file.sum {
		return vaultFile{}, false, nil
	}
	file, err := parseVaultFile(data)
	if err != nil {
		return vaultFile{}, false, err
	}
	if file.header.generation < v.highest {
		return vaultFile{}, false, ErrRollback
	}
	return file, true, nil
}

func (v *Vault) syncLocked(current []byte) error {
	if current == nil {
		return nil
	}
	file, changed, err := v.changedFileLocked(current)
	if err != nil || !changed {
		return err
	}
	secrets, err := openPayload(file, v.dek)
	if err != nil {
		v.lockLocked()
		v.file = file
		v.noteGenerationLocked(file.header.generation)
		return ErrChangedElsewhere
	}
	v.adoptLocked(file, revision{dek: v.dek, secrets: secrets})
	return nil
}

func (v *Vault) seal(rev revision) ([]byte, vaultFile, error) {
	h := header{version: formatVersion, flags: v.file.header.flags, generation: v.highest + 1, slots: rev.slots}
	headerRaw, err := encodeHeader(h)
	if err != nil {
		return nil, vaultFile{}, err
	}
	aead, err := newXChaCha20Poly1305(rev.dek)
	if err != nil {
		return nil, vaultFile{}, fmt.Errorf("vault: %w", err)
	}
	plaintext, _ := json.Marshal(rev.secrets)
	defer clear(plaintext)
	nonce := make([]byte, payloadNonceSize)
	_, _ = rand.Read(nonce)
	ciphertext := aead.Seal(nil, nonce, plaintext, headerRaw)
	data := make([]byte, 0, len(headerRaw)+len(nonce)+len(ciphertext))
	data = append(data, headerRaw...)
	data = append(data, nonce...)
	data = append(data, ciphertext...)
	return data, vaultFile{
		header:     h,
		headerRaw:  headerRaw,
		nonce:      nonce,
		ciphertext: ciphertext,
		sum:        sha256.Sum256(data),
	}, nil
}

func (v *Vault) writeLocked(change func() (revision, error)) error {
	var (
		next    revision
		written vaultFile
	)
	err := safefile.Update(v.opts.Path, func(current []byte) ([]byte, error) {
		if err := v.syncLocked(current); err != nil {
			return nil, err
		}
		var err error
		if next, err = change(); err != nil {
			return nil, err
		}
		data, file, err := v.seal(next)
		written = file
		return data, err
	})
	if err != nil {
		v.discardLocked(next)
		return err
	}
	v.adoptLocked(written, next)
	return nil
}

func sameBuffer(a, b []byte) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

func (v *Vault) discardLocked(rev revision) {
	if rev.secrets != nil && rev.secrets != v.secrets {
		rev.secrets.wipe()
	}
	if !sameBuffer(rev.dek, v.dek) {
		clear(rev.dek)
	}
}

func (v *Vault) adoptLocked(file vaultFile, rev revision) {
	if v.secrets != nil && v.secrets != rev.secrets {
		v.secrets.wipe()
	}
	if !sameBuffer(v.dek, rev.dek) {
		clear(v.dek)
	}
	v.file = file
	v.dek = rev.dek
	v.secrets = rev.secrets
	v.noteGenerationLocked(file.header.generation)
}

func (v *Vault) noteGenerationLocked(generation uint64) {
	v.highest = max(v.highest, generation)
	if v.opts.OnGeneration != nil {
		v.opts.OnGeneration(v.highest)
	}
}

func (v *Vault) payloadRevisionLocked(next *payload) revision {
	return revision{slots: v.file.header.slots, dek: v.dek, secrets: next}
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
	if len(v.file.header.slots) >= maxSlotCount {
		return ErrTooManySlots
	}
	slot, err := unlocker.Wrap(ctx, v.dek)
	if err != nil {
		return err
	}
	err = v.writeLocked(func() (revision, error) {
		return revision{slots: append(slices.Clone(v.file.header.slots), slot), dek: v.dek, secrets: v.secrets}, nil
	})
	if err == nil {
		v.touchLocked()
	}
	return err
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
	err := v.writeLocked(func() (revision, error) {
		slots := v.file.header.slots
		if index < 0 || index >= len(slots) {
			return revision{}, ErrSlotIndex
		}
		if len(slots) <= minSlotCount {
			return revision{}, ErrLastSlot
		}
		return revision{slots: slices.Delete(slices.Clone(slots), index, index+1), dek: v.dek, secrets: v.secrets}, nil
	})
	if err == nil {
		v.touchLocked()
	}
	return err
}

func (v *Vault) Rekey(ctx context.Context, unlockers []Unlocker) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	return v.rekeyLocked(ctx, unlockers)
}

func (v *Vault) RevokeSlot(ctx context.Context, index int, others []Unlocker) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	slots := v.file.header.slots
	if index < 0 || index >= len(slots) {
		return ErrSlotIndex
	}
	if len(slots) <= minSlotCount {
		return ErrLastSlot
	}
	kept, err := v.keptUnlockersLocked(ctx, others, func(i int, _ Slot) bool { return i == index })
	if err != nil {
		return err
	}
	return v.rekeyLocked(ctx, kept)
}

func (v *Vault) ChangePassword(ctx context.Context, current, next *PasswordUnlocker, others []Unlocker) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if next == nil {
		return ErrNilUnlocker
	}
	if v.dek == nil {
		return ErrLocked
	}
	if err := v.verifyPasswordLocked(ctx, current); err != nil {
		return err
	}
	kept, err := v.keptUnlockersLocked(ctx, others, func(_ int, s Slot) bool { return s.Kind == SlotPassword })
	if err != nil {
		return err
	}
	return v.rekeyLocked(ctx, append(kept, next))
}

func (v *Vault) AcceptGeneration(known uint64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	return v.writeLocked(func() (revision, error) {
		v.highest = max(v.highest, known)
		return v.payloadRevisionLocked(v.secrets), nil
	})
}

func (v *Vault) verifyPasswordLocked(ctx context.Context, current *PasswordUnlocker) error {
	protected := false
	for _, slot := range v.file.header.slots {
		if slot.Kind != SlotPassword {
			continue
		}
		protected = true
		if current != nil && v.unwrapsToKeyLocked(ctx, current, slot) {
			return nil
		}
	}
	if protected {
		return ErrWrongKey
	}
	return nil
}

func (v *Vault) keptUnlockersLocked(ctx context.Context, others []Unlocker, drop func(int, Slot) bool) ([]Unlocker, error) {
	used := make(map[int]bool, len(others))
	kept := make([]Unlocker, 0, len(others)+1)
	for i, slot := range v.file.header.slots {
		if drop(i, slot) {
			continue
		}
		idx := v.unlockerForSlotLocked(ctx, others, slot)
		if idx < 0 {
			return nil, fmt.Errorf("%w: %s", ErrSlotUnavailable, slot.Kind)
		}
		if !used[idx] {
			used[idx] = true
			kept = append(kept, others[idx])
		}
	}
	return kept, nil
}

func (v *Vault) unlockerForSlotLocked(ctx context.Context, others []Unlocker, slot Slot) int {
	for i, u := range others {
		if u != nil && u.Kind() == slot.Kind && v.unwrapsToKeyLocked(ctx, u, slot) {
			return i
		}
	}
	return -1
}

func (v *Vault) unwrapsToKeyLocked(ctx context.Context, u Unlocker, slot Slot) bool {
	got, err := u.Unwrap(ctx, slot)
	if err != nil {
		return false
	}
	defer clear(got)
	return subtle.ConstantTimeCompare(got, v.dek) == 1
}

func (v *Vault) rekeyLocked(ctx context.Context, unlockers []Unlocker) error {
	if len(unlockers) < minSlotCount || len(unlockers) > maxSlotCount {
		return ErrSlotCount
	}
	dek := make([]byte, chacha20poly1305.KeySize)
	_, _ = rand.Read(dek)
	slots := make([]Slot, 0, len(unlockers))
	for _, u := range unlockers {
		slot, err := wrapVerified(ctx, u, dek)
		if err != nil {
			clear(dek)
			return err
		}
		slots = append(slots, slot)
	}
	err := v.writeLocked(func() (revision, error) {
		return revision{slots: slots, dek: dek, secrets: v.secrets}, nil
	})
	if err == nil {
		v.touchLocked()
	}
	return err
}

func wrapVerified(ctx context.Context, u Unlocker, dek []byte) (Slot, error) {
	if u == nil {
		return Slot{}, ErrNilUnlocker
	}
	slot, err := u.Wrap(ctx, dek)
	if err != nil {
		return Slot{}, err
	}
	got, err := u.Unwrap(ctx, slot)
	if err != nil {
		return Slot{}, err
	}
	defer clear(got)
	if subtle.ConstantTimeCompare(got, dek) != 1 {
		return Slot{}, ErrWrongKey
	}
	return slot, nil
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

func (v *Vault) mutateLocked(change func() (revision, error)) error {
	if v.closed {
		return ErrClosed
	}
	if v.dek == nil {
		return ErrLocked
	}
	err := v.writeLocked(change)
	if err == nil {
		v.touchLocked()
	}
	return err
}

func (v *Vault) SetCredential(c Credential) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.mutateLocked(func() (revision, error) {
		next := clonePayload(v.secrets)
		stored := cloneCredential(c)
		idx := slices.IndexFunc(next.Credentials, func(e Credential) bool { return e.Resource == c.Resource })
		if idx >= 0 {
			next.Credentials[idx].Wipe()
			next.Credentials[idx] = stored
		} else {
			next.Credentials = append(next.Credentials, stored)
		}
		return v.payloadRevisionLocked(next), nil
	})
}

func (v *Vault) DeleteCredential(resource string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.mutateLocked(func() (revision, error) {
		idx := slices.IndexFunc(v.secrets.Credentials, func(e Credential) bool { return e.Resource == resource })
		if idx < 0 {
			return revision{}, ErrNotFound
		}
		next := clonePayload(v.secrets)
		next.Credentials[idx].Wipe()
		next.Credentials = slices.Delete(next.Credentials, idx, idx+1)
		return v.payloadRevisionLocked(next), nil
	})
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
	return v.mutateLocked(func() (revision, error) {
		next := clonePayload(v.secrets)
		stored := cloneSSHKey(k)
		idx := slices.IndexFunc(next.SSHKeys, func(e SSHKey) bool { return e.Host == k.Host })
		if idx >= 0 {
			next.SSHKeys[idx].Wipe()
			next.SSHKeys[idx] = stored
		} else {
			next.SSHKeys = append(next.SSHKeys, stored)
		}
		return v.payloadRevisionLocked(next), nil
	})
}

func (v *Vault) DeleteSSHKey(host string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.mutateLocked(func() (revision, error) {
		idx := slices.IndexFunc(v.secrets.SSHKeys, func(e SSHKey) bool { return e.Host == host })
		if idx < 0 {
			return revision{}, ErrNotFound
		}
		next := clonePayload(v.secrets)
		next.SSHKeys[idx].Wipe()
		next.SSHKeys = slices.Delete(next.SSHKeys, idx, idx+1)
		return v.payloadRevisionLocked(next), nil
	})
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
