package refs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type updateKind uint8

const (
	kindSet updateKind = iota
	kindSymbolic
	kindDelete
	kindLog
)

type update struct {
	name     Name
	kind     updateKind
	value    hash.ObjectID
	target   Name
	old      hash.ObjectID
	checkOld bool
	deref    bool
	linked   *update
	lock     *lockFile
	existed  bool
	current  hash.ObjectID
	resolved hash.ObjectID
	skip     bool
}

type Transaction struct {
	store   *Store
	message string
	added   []*update
	names   map[Name]*update
	done    bool
}

func (s *Store) Begin() *Transaction {
	return &Transaction{store: s, names: make(map[Name]*update)}
}

func (t *Transaction) SetMessage(message string) { t.message = message }

func (t *Transaction) add(entry *update) error {
	if t.done {
		return ErrCommitted
	}
	if err := entry.name.Validate(); err != nil {
		return err
	}
	if entry.kind != kindDelete && entry.kind != kindSymbolic && entry.value.IsZero() {
		return fmt.Errorf("%w: %s", ErrInvalidTarget, entry.name)
	}
	if _, exists := t.names[entry.name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateUpdate, entry.name)
	}
	t.names[entry.name] = entry
	t.added = append(t.added, entry)
	return nil
}

func (t *Transaction) Update(name Name, newValue, oldValue hash.ObjectID) error {
	return t.add(&update{name: name, value: newValue, old: oldValue, checkOld: true, deref: true})
}

func (t *Transaction) Set(name Name, newValue hash.ObjectID) error {
	return t.add(&update{name: name, value: newValue, deref: true})
}

func (t *Transaction) Detach(name Name, newValue hash.ObjectID) error {
	return t.add(&update{name: name, value: newValue})
}

func (t *Transaction) SetSymbolic(name, target Name) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if !strings.HasPrefix(string(target), RefsPrefix) {
		return fmt.Errorf("%w: %s", ErrSymbolicOutsideRefs, target)
	}
	return t.add(&update{name: name, kind: kindSymbolic, target: target})
}

func (t *Transaction) Delete(name Name, oldValue hash.ObjectID) error {
	return t.add(&update{name: name, kind: kindDelete, old: oldValue, checkOld: !oldValue.IsZero()})
}

func (t *Transaction) Rollback() {
	t.done = true
}

func (t *Transaction) Commit() error {
	if t.done {
		return ErrCommitted
	}
	t.done = true
	plan, err := t.plan()
	if err != nil {
		return err
	}
	locked := make([]*update, 0, len(plan))
	defer func() {
		for _, entry := range locked {
			entry.lock.release()
		}
	}()
	snapshot, err := t.store.loadPacked()
	if err != nil {
		return err
	}
	for _, entry := range plan {
		if entry.kind == kindSet || entry.kind == kindSymbolic {
			if err := t.store.checkAvailable(entry.name, snapshot); err != nil {
				return err
			}
		}
		lock, err := newLock(t.store.treeFor(entry.name), string(entry.name))
		if err != nil {
			return err
		}
		entry.lock = lock
		locked = append(locked, entry)
	}
	for _, entry := range plan {
		if err := t.read(entry, snapshot); err != nil {
			return err
		}
	}
	for _, entry := range plan {
		if err := verify(entry); err != nil {
			return err
		}
	}
	for _, entry := range plan {
		if err := t.writeLock(entry); err != nil {
			return err
		}
	}
	return t.finish(plan, snapshot)
}

func (t *Transaction) plan() ([]*update, error) {
	plan := make([]*update, 0, len(t.added)+1)
	for _, entry := range t.added {
		if entry.kind == kindSet && entry.deref {
			final, err := t.store.ResolveName(entry.name)
			if err != nil {
				return nil, err
			}
			if final != entry.name {
				plan = append(plan, &update{name: entry.name, kind: kindLog, linked: entry})
				entry.name = final
			}
		}
		plan = append(plan, entry)
	}
	planned := make(map[Name]*update, len(plan)+1)
	for _, entry := range plan {
		if _, exists := planned[entry.name]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateUpdate, entry.name)
		}
		planned[entry.name] = entry
	}
	head, err := t.store.headTarget()
	if err != nil {
		return nil, err
	}
	if _, exists := planned[HEAD]; head != "" && !exists {
		for _, entry := range slices.Clone(plan) {
			if entry.kind == kindSet && entry.name == head {
				plan = append(plan, &update{name: HEAD, kind: kindLog, linked: entry})
				break
			}
		}
	}
	if err := checkPlanConflicts(plan, planned); err != nil {
		return nil, err
	}
	slices.SortFunc(plan, func(a, b *update) int {
		return strings.Compare(string(a.name), string(b.name))
	})
	return plan, nil
}

func checkPlanConflicts(plan []*update, planned map[Name]*update) error {
	for _, entry := range plan {
		if entry.kind == kindDelete {
			continue
		}
		for index, current := range string(entry.name) {
			if current != '/' {
				continue
			}
			other, ok := planned[Name(string(entry.name)[:index])]
			if ok && other.kind != kindDelete {
				return fmt.Errorf("%w: %s and %s in one transaction",
					ErrNameConflict, other.name, entry.name)
			}
		}
	}
	return nil
}

func (s *Store) headTarget() (Name, error) {
	head, err := s.Lookup(HEAD)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !head.IsSymbolic() {
		return "", nil
	}
	return s.ResolveName(HEAD)
}

func (t *Transaction) read(entry *update, snapshot *packedSnapshot) error {
	ref, err := t.store.looseRef(entry.name)
	if errors.Is(err, ErrNotFound) {
		ref, err = t.store.packedRef(entry.name, snapshot)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
	}
	if err != nil {
		return err
	}
	entry.existed = true
	entry.current = ref.Target
	entry.resolved = ref.Target
	if ref.IsSymbolic() {
		resolved, err := t.store.resolveTarget(ref.SymbolicTarget)
		if err != nil {
			return err
		}
		entry.resolved = resolved
	}
	return nil
}

func verify(entry *update) error {
	if entry.kind == kindDelete && !entry.existed && !entry.checkOld {
		entry.skip = true
		return nil
	}
	if !entry.checkOld {
		return nil
	}
	if entry.old.IsZero() {
		if entry.existed {
			return fmt.Errorf("%w: %s already exists", ErrOldValueMismatch, entry.name)
		}
		return nil
	}
	if !entry.existed {
		return fmt.Errorf("%w: %s does not exist", ErrOldValueMismatch, entry.name)
	}
	if entry.current != entry.old {
		return fmt.Errorf("%w: %s is at %s instead of %s",
			ErrOldValueMismatch, entry.name, entry.current, entry.old)
	}
	return nil
}

func (t *Transaction) writeLock(entry *update) error {
	switch entry.kind {
	case kindSet:
		return entry.lock.write([]byte(entry.value.String() + "\n"))
	case kindSymbolic:
		return entry.lock.write([]byte(symbolicPrefix + " " + string(entry.target) + "\n"))
	}
	return nil
}

func (t *Transaction) finish(plan []*update, snapshot *packedSnapshot) error {
	for _, entry := range plan {
		if entry.kind == kindDelete || entry.skip {
			continue
		}
		if err := t.writeReflog(entry); err != nil {
			return err
		}
		if entry.kind == kindLog {
			continue
		}
		if err := entry.lock.commit(); err != nil {
			return err
		}
	}
	if err := t.store.unpack(plan, snapshot); err != nil {
		return err
	}
	for _, entry := range plan {
		if entry.kind != kindDelete || entry.skip {
			continue
		}
		from := t.store.treeFor(entry.name)
		if err := from.remove(string(entry.name)); err != nil {
			return err
		}
		if err := t.store.removeReflog(entry.name); err != nil {
			return err
		}
		entry.lock.release()
		from.removeEmptyDirs(string(entry.name), keepRefDirs)
	}
	return nil
}

func (t *Transaction) writeReflog(entry *update) error {
	old, value := entry.resolved, entry.value
	switch entry.kind {
	case kindLog:
		old, value = entry.linked.resolved, entry.linked.value
	case kindSymbolic:
		if t.message == "" {
			return nil
		}
		target, err := t.store.resolveTarget(entry.target)
		if err != nil {
			return err
		}
		value = target
	}
	return t.store.appendReflog(entry.name, old, value, t.message)
}

func (s *Store) unpack(plan []*update, snapshot *packedSnapshot) error {
	var removed []Name
	for _, entry := range plan {
		if entry.kind != kindDelete || entry.skip {
			continue
		}
		if _, ok := snapshot.find(entry.name); ok {
			removed = append(removed, entry.name)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	kept := slices.DeleteFunc(slices.Clone(snapshot.refs), func(ref Ref) bool {
		return slices.Contains(removed, ref.Name)
	})
	return s.writePacked(kept, snapshot.fullyPeeled)
}

func (s *Store) writePacked(refs []Ref, peeled bool) error {
	lock, err := newLock(s.common, packedRefsFile)
	if err != nil {
		return err
	}
	defer lock.release()
	if err := lock.write(encodePackedRefs(refs, peeled)); err != nil {
		return err
	}
	return lock.commit()
}

func (s *Store) checkAvailable(name Name, snapshot *packedSnapshot) error {
	from := s.treeFor(name)
	for index, current := range string(name) {
		if current != '/' {
			continue
		}
		prefix := Name(string(name)[:index])
		if from.isFile(string(prefix)) {
			return fmt.Errorf("%w: %s exists, %s cannot", ErrNameConflict, prefix, name)
		}
		if _, ok := snapshot.find(prefix); ok {
			return fmt.Errorf("%w: %s is packed, %s cannot exist", ErrNameConflict, prefix, name)
		}
	}
	if snapshot.hasPrefix(string(name) + "/") {
		return fmt.Errorf("%w: packed references live under %s", ErrNameConflict, name)
	}
	return from.clearEmptyTree(string(name))
}
