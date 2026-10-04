package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	fileMode = 0o600
	dirMode  = 0o700
)

// errCorrupt marks a state file that exists but cannot be decoded.
var errCorrupt = errors.New("state file is corrupt")

// writeAtomic replaces path with data: write a temp file in the same
// directory, fsync it, rename over path, then fsync the directory. A crash
// leaves either the old or the new content, never a torn file.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp state file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return fmt.Errorf("rename state file: %w", err)
	}
	syncDir(dir)
	return nil
}

// syncDir best-effort fsyncs a directory so the rename is durable.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// readJSON decodes path into v. A missing file returns exists=false. Unreadable
// bytes return an error wrapping errCorrupt; other I/O errors are returned as is.
func readJSON(path string, v any) (exists bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read state file: %w", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return true, fmt.Errorf("%w: %v", errCorrupt, err)
	}
	return true, nil
}

// writeJSON marshals v and writes it atomically.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return fmt.Errorf("encode state file: %w", err)
	}
	return writeAtomic(path, append(b, '\n'))
}

// quarantine renames a corrupt file to <path>.corrupt-<ts> so it can be
// inspected and the store can start fresh. It returns the new name.
func quarantine(path string, now time.Time) (string, error) {
	base := path + ".corrupt-" + now.UTC().Format("20060102T150405Z")
	name := base
	for i := 1; ; i++ {
		if _, err := os.Lstat(name); errors.Is(err, os.ErrNotExist) {
			break
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	if err := os.Rename(path, name); err != nil {
		return "", fmt.Errorf("quarantine corrupt state file: %w", err)
	}
	return name, nil
}
