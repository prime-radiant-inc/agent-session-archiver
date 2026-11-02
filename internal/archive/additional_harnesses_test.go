package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLacePreservesDistinctFilesWithoutInventingProjects(t *testing.T) {
	home := t.TempDir()
	for _, day := range []string{"2026-10-26", "2026-10-27"} {
		put(t, filepath.Join(home, ".lace/transcripts/_unknown", day, "session.jsonl"), "{\"type\":\"message\",\"data\":{\"content\":\"raw\"}}\n{broken\n")
	}
	inv := Discover(context.Background(), Host{User: "u", Host: "h", Harnesses: map[string]Harness{"lace": {}}}, home)
	if len(inv.Errors) != 0 || len(inv.Traces) != 2 {
		t.Fatalf("%+v", inv)
	}
	if inv.Traces[0].ID == inv.Traces[1].ID {
		t.Fatal("different files collided")
	}
	for _, m := range inv.Traces {
		if m.Project != Component("unknown") || len(m.Members) != 1 {
			t.Fatalf("%+v", m)
		}
	}
	root := t.TempDir()
	batch, e := NewIncoming(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = Prepare(context.Background(), batch, inv.Traces); e != nil {
		t.Fatal(e)
	}
	if e = Incorporate(root, batch); e != nil {
		t.Fatal(e)
	}
	if e = os.RemoveAll(filepath.Join(home, ".lace")); e != nil {
		t.Fatal(e)
	}
	if e = Verify(root); e != nil {
		t.Fatal(e)
	}
}

func TestGrokCapturesWholeSessionAndRetainsTranscript(t *testing.T) {
	home := t.TempDir()
	session := filepath.Join(home, ".grok/sessions/%2Fgone/session-id")
	put(t, filepath.Join(session, "summary.json"), `{"info":{"id":"session-id","cwd":"/gone"},"git_remotes":["git@github.com:owner/repo.git"]}`)
	put(t, filepath.Join(session, "chat_history.jsonl"), "first\n")
	put(t, filepath.Join(session, "updates.jsonl"), "update\n")
	put(t, filepath.Join(session, "terminal/output.log"), "output")
	put(t, filepath.Join(session, "images/image.png"), "image bytes")
	put(t, filepath.Join(session, "chat_history.jsonl.lock"), "")
	put(t, filepath.Join(home, ".grok/sessions/prompt_history.jsonl"), "not a session")
	h := Host{User: "u", Host: "h", Harnesses: map[string]Harness{"grok": {}}}
	inv := Discover(context.Background(), h, home)
	if len(inv.Errors) != 0 || len(inv.Traces) != 1 {
		t.Fatalf("%+v", inv)
	}
	m := inv.Traces[0]
	if len(m.Members) != 5 || m.Project != Component("github.com/owner/repo") || m.NativeID != "session-id" {
		t.Fatalf("%+v", m)
	}
	root := t.TempDir()
	capture := func() error {
		t.Helper()
		inv = Discover(context.Background(), h, home)
		if len(inv.Errors) != 0 {
			t.Fatal(inv.Errors)
		}
		b, e := NewIncoming(root)
		if e != nil {
			t.Fatal(e)
		}
		if e = Prepare(context.Background(), b, inv.Traces); e != nil {
			t.Fatal(e)
		}
		return Incorporate(root, b)
	}
	if e := capture(); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(session, "summary.json"), `{"info":{"cwd":"/gone"},"title":"new"}`)
	put(t, filepath.Join(session, "chat_history.jsonl"), "first\nsecond\n")
	if e := capture(); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(session, "chat_history.jsonl"), "first\n")
	if e := capture(); e == nil {
		t.Fatal("transcript shrink accepted")
	}
	if e := os.RemoveAll(filepath.Join(home, ".grok")); e != nil {
		t.Fatal(e)
	}
	if e := Verify(root); e != nil {
		t.Fatal(e)
	}
}
