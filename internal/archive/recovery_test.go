package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverCompleteTransferBeforeIncorporation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	src := filepath.Join(t.TempDir(), "source")
	b, m := makeBatch(t, root, src, "captured\n")
	os.Remove(src)
	if e := Recover(root); e != nil {
		t.Fatal(e)
	}
	all, e := LoadManifests(root)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := all[m.ID]; !ok {
		t.Fatalf("fully transferred batch ignored after source deletion: %s", b)
	}
}
func TestRecoveryDoesNotBlockOtherHosts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	src := filepath.Join(t.TempDir(), "source")
	b, _ := makeBatch(t, root, src, "old\n")
	if e := Incorporate(root, b); e != nil {
		t.Fatal(e)
	}
	b, _ = makeBatch(t, root, src, "conflicting\n")
	if e := Incorporate(root, b); e == nil {
		t.Fatal("expected conflict")
	}
	m := Manifest{Version: 1, User: "u", Host: "healthy", Harness: "claude", Root: "/healthy", NativeID: "new", Project: "project"}
	m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, m.NativeID)
	if _, e := handle(context.Background(), request{Op: "select", Host: Host{User: "u", Host: "healthy"}, Root: root, Traces: []Manifest{m}}); e != nil {
		t.Fatalf("healthy host blocked by h's retained conflict: %v", e)
	}
}
func TestConcurrentFirstCaptureRetainsPlacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	src := filepath.Join(t.TempDir(), "source")
	b, m := makeBatch(t, root, src, "same\n")
	incoming, _ := ReadBatch(b)
	incoming[0].Project = "other-project"
	incoming[0].Members[0].SHA256 = ""
	other, _ := NewIncoming(root)
	if e := Prepare(context.Background(), other, incoming); e != nil {
		t.Fatal(e)
	}
	if e := Incorporate(root, b); e != nil {
		t.Fatal(e)
	}
	if e := Incorporate(root, other); e != nil {
		t.Fatal(e)
	}
	if e := Verify(root); e != nil {
		t.Fatalf("concurrently selected first captures split identity %s: %v", m.ID, e)
	}
}
