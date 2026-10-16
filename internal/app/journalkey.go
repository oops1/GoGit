package app

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	"github.com/oops1/gogit/internal/ui/branches"
)

type journalKeeper struct {
	mu  sync.Mutex
	key string
}

func (k *journalKeeper) changed(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.key == key {
		return false
	}
	k.key = key
	return true
}

func (k *journalKeeper) remember(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.key = key
}

func journalKeyOf(snap branches.Snapshot, hasRemotes bool, logMaxCount int) string {
	var text strings.Builder
	fmt.Fprintf(&text, "%s|%t|%s|%t|%d\n", snap.Current, snap.Detached, snap.HeadID, hasRemotes, logMaxCount)
	for _, branch := range snap.Local {
		fmt.Fprintf(&text, "L|%s|%s|%s\n", branch.Name, branch.Target, branch.SymbolicTarget)
	}
	for _, remote := range snap.Remotes {
		fmt.Fprintf(&text, "R|%s|%s\n", remote.Name, remote.Head)
		for _, branch := range remote.Branches {
			fmt.Fprintf(&text, "B|%s|%s\n", branch.Name, branch.Target)
		}
	}
	for _, tag := range snap.Tags {
		fmt.Fprintf(&text, "T|%s|%s|%s\n", tag.Name, tag.Target, tag.Peeled)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(text.String())))
}

func (a *App) currentJournalKey(snap branches.Snapshot) string {
	return journalKeyOf(snap, a.State().HasRemotes, a.cfg.Git.LogMaxCount)
}
