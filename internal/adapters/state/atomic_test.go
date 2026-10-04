package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteAtomicReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	for _, content := range []string{"one", "two-longer"} {
		if err := writeAtomic(p, []byte(content)); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != content {
			t.Fatalf("got %q", got)
		}
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("temp files left behind: %v", ents)
	}
}

func TestWriteAtomicErrors(t *testing.T) {
	if err := writeAtomic(filepath.Join(t.TempDir(), "missing", "f.json"), []byte("x")); err == nil {
		t.Fatal("want error for missing dir")
	}
	// A rename target that is a non-empty directory fails and cleans the temp.
	dir := t.TempDir()
	target := filepath.Join(dir, "f.json")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(target, []byte("x")); err == nil {
		t.Fatal("want rename error")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("temp not cleaned: %v", ents)
	}
}

func TestReadJSON(t *testing.T) {
	dir := t.TempDir()
	var v struct{ A int }
	if ok, err := readJSON(filepath.Join(dir, "none"), &v); ok || err != nil {
		t.Fatalf("missing: %v %v", ok, err)
	}
	writeFile(t, filepath.Join(dir, "good"), `{"A":3}`)
	if ok, err := readJSON(filepath.Join(dir, "good"), &v); !ok || err != nil || v.A != 3 {
		t.Fatalf("good: %v %v %v", ok, err, v)
	}
	for name, content := range map[string]string{"empty": "", "garbage": "{not json", "truncated": `{"A":`} {
		writeFile(t, filepath.Join(dir, name), content)
		if _, err := readJSON(filepath.Join(dir, name), &v); !errors.Is(err, errCorrupt) {
			t.Fatalf("%s: want errCorrupt, got %v", name, err)
		}
	}
	if _, err := readJSON(dir, &v); err == nil || errors.Is(err, errCorrupt) {
		t.Fatalf("directory read must be a plain I/O error, got %v", err)
	}
}

func TestQuarantine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	now := time.Date(2026, 10, 3, 12, 0, 5, 0, time.UTC)
	var names []string
	for range 2 {
		writeFile(t, p, "junk")
		n, err := quarantine(p, now)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, filepath.Base(n))
	}
	if names[0] != "x.json.corrupt-20261003T120005Z" || names[1] != "x.json.corrupt-20261003T120005Z-1" {
		t.Fatalf("names = %v", names)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("original must be gone")
	}
	if _, err := quarantine(p, now); err == nil || !strings.Contains(err.Error(), "quarantine") {
		t.Fatalf("missing file: %v", err)
	}
}

func TestSyncDirMissingIsHarmless(t *testing.T) {
	syncDir(filepath.Join(t.TempDir(), "nope"))
}
