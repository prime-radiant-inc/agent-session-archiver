package archive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func makeBatch(t *testing.T, root, source, content string) (string, Manifest) {
	t.Helper()
	put(t, source, content)
	m := Manifest{Version: 1, User: "u", Host: "h", Harness: "claude", Root: filepath.Dir(source), NativeID: "id", Project: "project"}
	m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, m.NativeID)
	if e := addMember(&m, source, "session.jsonl", false); e != nil {
		t.Fatal(e)
	}
	b, e := NewIncoming(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = Prepare(context.Background(), b, []Manifest{m}); e != nil {
		t.Fatal(e)
	}
	return b, m
}
func TestArchiveRetention(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	src := filepath.Join(t.TempDir(), "source")
	b, m := makeBatch(t, root, src, "first\n")
	if e := Incorporate(root, b); e != nil {
		t.Fatal(e)
	}
	b, _ = makeBatch(t, root, src, "first\nsecond\n")
	if e := Incorporate(root, b); e != nil {
		t.Fatal(e)
	}
	for _, content := range []string{"first\n", "WRONG\nsecond\n"} {
		b, _ = makeBatch(t, root, src, content)
		if e := Incorporate(root, b); e == nil {
			t.Fatal("destructive change accepted")
		}
	}
	dest := filepath.Join(root, m.Dir(), "raw/session.jsonl")
	got, _ := os.ReadFile(dest)
	if string(got) != "first\nsecond\n" {
		t.Fatal("lost archive bytes")
	}
	os.Remove(src)
	if e := Verify(root); e != nil {
		t.Fatal(e)
	}
	put(t, dest, "corruption")
	if e := Verify(root); e == nil {
		t.Fatal("corruption undetected")
	}
}
func TestRecoverWithoutSource(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	src := filepath.Join(t.TempDir(), "source")
	b, m := makeBatch(t, root, src, "first\n")
	if e := Incorporate(root, b); e != nil {
		t.Fatal(e)
	}
	b, _ = makeBatch(t, root, src, "first\nsecond\n")
	if e := MarkReady(root, b); e != nil {
		t.Fatal(e)
	}
	// Simulate interruption after a file rename but before manifest publication.
	raw, _ := os.ReadFile(filepath.Join(b, m.Dir(), "raw/session.jsonl"))
	put(t, filepath.Join(root, m.Dir(), "raw/session.jsonl"), string(raw))
	os.Remove(src)
	if e := Recover(root); e != nil {
		t.Fatal(e)
	}
	if e := Verify(root); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(b); !os.IsNotExist(e) {
		t.Fatal("completed staging not removed")
	}
}
func TestDestinationValidationAndLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	src := filepath.Join(t.TempDir(), "source")
	b, m := makeBatch(t, root, src, "first\n")
	release, e := lockHost(root, "u", "h")
	if e != nil {
		t.Fatal(e)
	}
	if e = Incorporate(root, b); e == nil {
		t.Fatal("concurrent writer admitted")
	}
	release()
	b, _ = makeBatch(t, root, src, "first\n")
	traces, _ := ReadBatch(b)
	traces[0].Members[0].Path = "../../escape"
	writeJSON(filepath.Join(b, "batch.json"), traces)
	if e = Incorporate(root, b); e == nil {
		t.Fatal("traversal accepted")
	}
	b, _ = makeBatch(t, root, src, "first\n")
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "u"), 0700)
	if e = os.Symlink(outside, filepath.Join(root, "u/h")); e != nil {
		t.Fatal(e)
	}
	if e = Incorporate(root, b); e == nil {
		t.Fatal("archive symlink accepted")
	}
	_ = m
}
func TestDiskFullLeavesCommittedData(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	src := filepath.Join(t.TempDir(), "source")
	b, m := makeBatch(t, root, src, "old\n")
	if e := Incorporate(root, b); e != nil {
		t.Fatal(e)
	}
	b, _ = makeBatch(t, root, src, "old\nnew\n")
	e := incorporate(root, b, func(string, string) error { return syscall.ENOSPC })
	if !errors.Is(e, syscall.ENOSPC) {
		t.Fatalf("wanted disk-full, got %v", e)
	}
	got, _ := os.ReadFile(filepath.Join(root, m.Dir(), "raw/session.jsonl"))
	if string(got) != "old\n" {
		t.Fatal("old bytes lost")
	}
	if e = Verify(root); e != nil {
		t.Fatal(e)
	}
	if e = Recover(root); e != nil {
		t.Fatal(e)
	}
	if e = Verify(root); e != nil {
		t.Fatal(e)
	}
}
