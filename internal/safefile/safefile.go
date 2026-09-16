package safefile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

var ErrLockTimeout = errors.New("safefile: another process keeps the file locked")

var (
	lockTimeout  = 10 * time.Second
	lockInterval = 20 * time.Millisecond
)

type Lock struct {
	file *os.File
}

func LockPath(path string) string {
	return path + ".lock"
}

func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("safefile: %w", err)
	}
	file, err := os.OpenFile(LockPath(path), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("safefile: %w", err)
	}
	deadline := time.Now().Add(lockTimeout)
	for !tryLock(file) {
		if !time.Now().Before(deadline) {
			_ = file.Close()
			return nil, ErrLockTimeout
		}
		time.Sleep(lockInterval)
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Release() error {
	return l.file.Close()
}

func Read(path string) ([]byte, error) {
	lock, err := Acquire(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()
	return os.ReadFile(path)
}

func Update(path string, change func(current []byte) ([]byte, error)) error {
	lock, err := Acquire(path)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("safefile: %w", err)
	}
	next, err := change(current)
	if err != nil || next == nil {
		return err
	}
	return WriteFile(path, next)
}

func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("safefile: %w", err)
	}
	tmp := file.Name()
	_, err = file.Write(data)
	err = errors.Join(err, file.Sync(), file.Close())
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return fmt.Errorf("safefile: %w", errors.Join(err, os.Remove(tmp)))
	}
	return wrap(syncDir(dir))
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("safefile: %w", err)
}
