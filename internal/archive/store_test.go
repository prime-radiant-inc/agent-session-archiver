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
	if e = stageBatch(context.Background(), b, []Manifest{m}); e != nil {
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
	dest := filepath.Join(root, m.Dir(), "raw/session.jsonl")
	// A rewritten or truncated transcript becomes current; each replaced version is
	// kept beside it under its checksum and recorded in the manifest.
	previous := "first\nsecond\n"
	for _, content := range []string{"WRONG\nsecond\n", "first\n"} {
		b, current := makeBatch(t, root, src, content)
		if e := Incorporate(root, b); e != nil {
			t.Fatalf("rewrite to %q: %v", content, e)
		}
		if got, _ := os.ReadFile(dest); string(got) != content {
			t.Fatalf("current bytes %q, want %q", got, content)
		}
		kept := filepath.Join(root, m.Dir(), "raw", supersededPath("session.jsonl", digest(previous)))
		if got, _ := os.ReadFile(kept); string(got) != previous {
			t.Fatalf("superseded bytes %q, want %q", got, previous)
		}
		archived, e := LoadManifests(root)
		if e != nil {
			t.Fatal(e)
		}
		if len(Select([]Manifest{current}, archived, false)) != 0 {
			t.Fatal("rewritten transcript selected again after incorporation")
		}
		previous = content
	}
	if all, _ := LoadManifests(root); len(all[m.ID].Members) != 3 {
		t.Fatalf("manifest members %+v", all[m.ID].Members)
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

func TestSharedCompanionsUseIndependentArchiveLinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	source := filepath.Join(t.TempDir(), "tool-result")
	put(t, source, "shared bytes")
	var traces []Manifest
	for _, id := range []string{"one", "two"} {
		m := Manifest{Version: 1, User: "u", Host: "h", Harness: "claude", Root: filepath.Dir(source), NativeID: id, Project: "project"}
		m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, id)
		addMember(&m, source, "tool.txt", true)
		traces = append(traces, m)
	}
	batch, _ := NewIncoming(root)
	if e := stageBatch(context.Background(), batch, traces); e != nil {
		t.Fatal(e)
	}
	first := filepath.Join(batch, traces[0].Dir(), "raw/tool.txt")
	second := filepath.Join(batch, traces[1].Dir(), "raw/tool.txt")
	a, _ := os.Stat(first)
	b, _ := os.Stat(second)
	s, _ := os.Stat(source)
	if !os.SameFile(a, b) {
		t.Fatal("shared companion copied twice in staging")
	}
	if os.SameFile(a, s) {
		t.Fatal("linked live source")
	}
	if e := Incorporate(root, batch); e != nil {
		t.Fatal(e)
	}
	first = filepath.Join(root, traces[0].Dir(), "raw/tool.txt")
	second = filepath.Join(root, traces[1].Dir(), "raw/tool.txt")
	a, _ = os.Stat(first)
	b, _ = os.Stat(second)
	if !os.SameFile(a, b) {
		t.Fatal("shared companion copied twice in archive")
	}
	put(t, source, "new result")
	m := traces[0]
	m.Members = nil
	addMember(&m, source, "tool.txt", true)
	batch, _ = NewIncoming(root)
	if e := stageBatch(context.Background(), batch, []Manifest{m}); e != nil {
		t.Fatal(e)
	}
	if e := Incorporate(root, batch); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(second)
	if string(got) != "shared bytes" {
		t.Fatal("update changed another trace's retained bytes")
	}
	if e := Verify(root); e != nil {
		t.Fatal(e)
	}
}
func TestInterruptedRewriteKeepsSupersededVersion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	src := filepath.Join(t.TempDir(), "source")
	b, m := makeBatch(t, root, src, "original\n")
	if e := Incorporate(root, b); e != nil {
		t.Fatal(e)
	}
	b, _ = makeBatch(t, root, src, "rewritten\n")
	crash := errors.New("crash before publishing")
	if e := incorporate(root, b, func(string, string) error { return crash }); !errors.Is(e, crash) {
		t.Fatalf("interrupted incorporation: %v", e)
	}
	// The original must already be recorded and verifiable before the current path changes.
	if e := Verify(root); e != nil {
		t.Fatal(e)
	}
	if all, _ := LoadManifests(root); len(all[m.ID].Members) != 2 {
		t.Fatalf("superseded version not recorded before publishing: %+v", all[m.ID].Members)
	}
	if e := Recover(root); e != nil {
		t.Fatal(e)
	}
	if got, _ := os.ReadFile(filepath.Join(root, m.Dir(), "raw/session.jsonl")); string(got) != "rewritten\n" {
		t.Fatalf("current bytes %q", got)
	}
	if all, _ := LoadManifests(root); len(all[m.ID].Members) != 2 {
		t.Fatalf("replay duplicated the superseded version: %+v", all[m.ID].Members)
	}
	if e := Verify(root); e != nil {
		t.Fatal(e)
	}
}
