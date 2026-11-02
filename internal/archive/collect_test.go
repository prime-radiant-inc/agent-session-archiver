package archive

import (
	"context"
	"encoding/json"
	"github.com/klauspost/compress/zstd"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordedRepositoriesSurviveMissingWorktrees(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "codex")
	missing := filepath.Join(d, "removed-worktree")
	for _, item := range []struct{ id, cwd, remote string }{
		{"first", missing, "git@github.com:owner/first.git"},
		{"second", missing, "https://github.com/owner/second.git"},
		{"same-repo", filepath.Join(d, "another-removed-worktree"), "https://github.com/owner/first.git"},
		{"no-evidence", missing, ""},
	} {
		b, err := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{
			"id": item.id, "cwd": item.cwd, "git": map[string]string{"repository_url": item.remote},
		}})
		if err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(root, "sessions", item.id+".jsonl"), string(b)+"\n")
	}
	inv := Discover(context.Background(), Host{User: "u", Host: "h", Harnesses: map[string]Harness{"codex": {Directories: []string{root}}}}, d)
	if len(inv.Errors) != 0 || len(inv.Traces) != 4 {
		t.Fatalf("%+v", inv)
	}
	want := map[string]string{
		"first": Component("github.com/owner/first"), "same-repo": Component("github.com/owner/first"),
		"second": Component("github.com/owner/second"), "no-evidence": Component(missing),
	}
	for _, m := range inv.Traces {
		if m.Project != want[m.NativeID] {
			t.Errorf("%s: project %q, want %q", m.NativeID, m.Project, want[m.NativeID])
		}
	}
}

func TestEvenerUsesRecordedRepository(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "evener")
	p := filepath.Join(root, "projects", "project", "sessions", "session")
	b, err := json.Marshal(map[string]string{"session_id": "session", "working_dir": filepath.Join(d, "removed-worktree")})
	if err != nil {
		t.Fatal(err)
	}
	put(t, p+".transcript.jsonl", string(b)+"\n")
	put(t, p+".meta.json", `{"env_info":{"git_origin_url":"git@github.com:owner/repo.git"}}`)
	inv := Discover(context.Background(), Host{User: "u", Host: "h", Harnesses: map[string]Harness{"evener": {Directories: []string{root}}}}, d)
	if len(inv.Errors) != 0 || len(inv.Traces) != 1 {
		t.Fatalf("%+v", inv)
	}
	if inv.Traces[0].Project != Component("github.com/owner/repo") || len(inv.Traces[0].Members) != 2 {
		t.Fatalf("recorded repository or raw companion lost: %+v", inv.Traces[0])
	}
}

func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestFileHarnessCollection(t *testing.T) {
	d := t.TempDir()
	put(t, filepath.Join(d, "claude/projects/p/s.jsonl"), "{\"sessionId\":\"s\",\"cwd\":\"/project\"}\n{malformed\n")
	put(t, filepath.Join(d, "claude/projects/p/s/tool-results/result.txt"), "tool result")
	put(t, filepath.Join(d, "claude2/projects/p/s.jsonl"), "{}\n")
	put(t, filepath.Join(d, "codex/sessions/2026/01/01/rollout.jsonl"), "{\"type\":\"session_meta\",\"payload\":{\"id\":\"c\",\"cwd\":\"/project\"}}\n")
	put(t, filepath.Join(d, "pi/sessions/p/session.jsonl"), "{\"type\":\"session\",\"id\":\"pi-id\",\"cwd\":\"/project\"}\n")
	put(t, filepath.Join(d, "evener/projects/p/sessions/e.transcript.jsonl"), "{\"kind\":\"header\",\"session_id\":\"e\",\"working_dir\":\"/project\"}\n{broken\n")
	put(t, filepath.Join(d, "evener/projects/p/sessions/e.meta.json"), "{\"a\":1}")
	// Discovery must not descend into Evener project worktrees.
	put(t, filepath.Join(d, "evener/projects/p/worktrees/x/unrelated.jsonl"), "{}")
	h := Host{User: "u", Host: "h", Harnesses: map[string]Harness{}}
	for _, name := range []string{"claude", "codex", "pi", "evener"} {
		h.Harnesses[name] = Harness{Directories: []string{filepath.Join(d, name)}}
	}
	h.Harnesses["claude"] = Harness{Directories: []string{filepath.Join(d, "claude"), filepath.Join(d, "claude2")}}
	inv := Discover(context.Background(), h, d)
	if len(inv.Errors) != 0 || len(inv.Traces) != 5 {
		t.Fatalf("%+v", inv)
	}
	seen := map[string]bool{}
	for _, m := range inv.Traces {
		if seen[m.ID] {
			t.Fatal("collision")
		}
		seen[m.ID] = true
		if m.Harness == "evener" && (len(m.Members) != 2 || m.CWD != "/project") {
			t.Fatal("missing metadata")
		}
	}
	batch := filepath.Join(d, "batch")
	if err := Prepare(context.Background(), batch, inv.Traces); err != nil {
		t.Fatal(err)
	}
	for _, m := range inv.Traces {
		for _, v := range m.Members {
			want, _ := os.ReadFile(v.Source)
			got, e := os.ReadFile(filepath.Join(batch, m.Dir(), "raw", v.Path))
			if e != nil || string(got) != string(want) {
				t.Fatalf("raw mismatch %s %v", v.Source, e)
			}
		}
	}
	archived, err := ReadBatch(batch)
	if err != nil {
		t.Fatal(err)
	}
	old := map[string]Manifest{}
	for _, m := range archived {
		old[m.ID] = m
	}
	if s := Select(inv.Traces, old, false); len(s) != 0 {
		t.Fatal("unchanged selected")
	}
	if s := Select(inv.Traces, old, true); len(s) != 5 {
		t.Fatal("full omitted sources")
	}
	missing := Discover(context.Background(), Host{User: "u", Host: "h", Harnesses: map[string]Harness{"pi": {Directories: []string{filepath.Join(d, "missing")}}}}, d)
	if len(missing.Errors) != 1 {
		t.Fatal("missing root not reported")
	}
}
func TestBoundedCapture(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "source")
	put(t, p, "abc")
	f, e := os.Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	info, _ := f.Stat()
	// Replacing the pathname must not redirect the open capture.
	if e = os.Rename(p, p+".old"); e != nil {
		t.Fatal(e)
	}
	put(t, p, "evil")
	out := filepath.Join(d, "copy")
	if _, e = copyOpened(f, info, out, false); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "abc" {
		t.Fatal(string(got))
	}
	f2, _ := os.Open(p)
	defer f2.Close()
	i2, _ := f2.Stat()
	if e = os.Truncate(p, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = copyOpened(f2, i2, filepath.Join(d, "short"), false); e == nil {
		t.Fatal("short read accepted")
	}
	put(t, p, "abc")
	f3, _ := os.Open(p)
	defer f3.Close()
	i3, _ := f3.Stat()
	a, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	a.WriteString("def")
	a.Close()
	if _, e = copyOpened(f3, i3, filepath.Join(d, "append"), false); e != nil {
		t.Fatal(e)
	}
	got, _ = os.ReadFile(filepath.Join(d, "append"))
	if string(got) != "abc" {
		t.Fatal("unbounded read")
	}
	f3.Seek(0, 0)
	if _, e = copyOpened(f3, i3, filepath.Join(d, "mutable"), true); e == nil {
		t.Fatal("mutable change accepted")
	}
}

func TestSelectOnlyChangedMembers(t *testing.T) {
	d := t.TempDir()
	a := filepath.Join(d, "a")
	b := filepath.Join(d, "b")
	put(t, a, "one")
	put(t, b, "two")
	m := Manifest{Version: 1, User: "u", Host: "h", Harness: "claude", Root: d, NativeID: "id", Project: "project", ID: "id"}
	if e := addMember(&m, a, "a", false); e != nil {
		t.Fatal(e)
	}
	if e := addMember(&m, b, "b", true); e != nil {
		t.Fatal(e)
	}
	first := filepath.Join(d, "first")
	if e := Prepare(context.Background(), first, []Manifest{m}); e != nil {
		t.Fatal(e)
	}
	old, _ := ReadBatch(first)
	put(t, b, "changed")
	m.Members = nil
	addMember(&m, a, "a", false)
	addMember(&m, b, "b", true)
	selected := Select([]Manifest{m}, map[string]Manifest{m.ID: old[0]}, false)
	second := filepath.Join(d, "second")
	if e := Prepare(context.Background(), second, selected); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(second, m.Dir(), "raw/a")); !os.IsNotExist(e) {
		t.Fatal("unchanged member copied")
	}
}

func TestCompressedCodexAndSharedIndex(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "codex")
	p := filepath.Join(root, "sessions/rollout.jsonl.zst")
	z, e := zstd.NewWriter(nil)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	raw := z.EncodeAll([]byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"uuid\",\"cwd\":\"/project\"}}\n"), nil)
	put(t, p, string(raw))
	put(t, filepath.Join(root, "session_index.jsonl"), "{\"id\":\"uuid\",\"title\":\"title\"}\n")
	h := Host{User: "u", Host: "h", Harnesses: map[string]Harness{"codex": {Directories: []string{root}}}}
	inv := Discover(context.Background(), h, d)
	if len(inv.Errors) != 0 || len(inv.Traces) != 2 {
		t.Fatalf("%+v", inv)
	}
	var before Manifest
	for _, m := range inv.Traces {
		if m.NativeID == "uuid" {
			before = m
		}
	}
	if before.CWD != "/project" {
		t.Fatal("compressed metadata lost")
	}
	moved := filepath.Join(root, "archived_sessions/rollout.jsonl.zst")
	os.MkdirAll(filepath.Dir(moved), 0700)
	if e = os.Rename(p, moved); e != nil {
		t.Fatal(e)
	}
	after := Discover(context.Background(), h, d)
	found := false
	for _, m := range after.Traces {
		if m.NativeID == "uuid" {
			found = true
			if m.ID != before.ID {
				t.Fatal("archived move changed identity")
			}
		}
	}
	if !found {
		t.Fatal("moved session missing")
	}
}

func TestPartialDiscoveryAndInternalLinks(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "projects/p/a.jsonl")
	put(t, p, "{}\n")
	put(t, filepath.Join(root, "projects/p/z.jsonl"), "{}\n")
	if e := os.Symlink(p, filepath.Join(root, "projects/p/b.jsonl")); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	put(t, outside, "private")
	if e := os.Symlink(outside, filepath.Join(root, "projects/p/c.jsonl")); e != nil {
		t.Fatal(e)
	}
	h := Host{User: "u", Host: "h", Harnesses: map[string]Harness{"claude": {Directories: []string{root}}}}
	inv := Discover(context.Background(), h, root)
	if len(inv.Traces) != 3 || len(inv.Errors) != 1 {
		t.Fatalf("traces=%d errors=%v", len(inv.Traces), inv.Errors)
	}
}
func TestEvenerIgnoresSessionArtifactTrees(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "projects/p/sessions/e.transcript.jsonl"), "{\"session_id\":\"e\",\"working_dir\":\"/project\"}\n")
	link := filepath.Join(root, "projects/p/sessions/e/artifacts/node_modules")
	os.MkdirAll(filepath.Dir(link), 0700)
	os.Symlink(t.TempDir(), link)
	inv := Discover(context.Background(), Host{User: "u", Host: "h", Harnesses: map[string]Harness{"evener": {Directories: []string{root}}}}, root)
	if len(inv.Errors) != 0 || len(inv.Traces) != 1 {
		t.Fatal(inv)
	}
}

func TestCodexOwnIDPrecedesParentSessionID(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"child-one", "child-two"} {
		put(t, filepath.Join(root, "sessions", id+".jsonl"), "{\"type\":\"session_meta\",\"payload\":{\"id\":\""+id+"\",\"session_id\":\"parent\",\"cwd\":\"/project\"}}\n")
	}
	inv := Discover(context.Background(), Host{User: "u", Host: "h", Harnesses: map[string]Harness{"codex": {Directories: []string{root}}}}, root)
	if len(inv.Traces) != 2 || inv.Traces[0].ID == inv.Traces[1].ID {
		t.Fatal("children collapsed onto parent identity")
	}
}

func TestClaudeSubagentCompanions(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "projects/p/parent/subagents/agent-a.jsonl"), "{}\n")
	put(t, filepath.Join(root, "projects/p/parent/tool-results/result.jsonl"), "{\"tool\":\"output\"}\n")
	inv := Discover(context.Background(), Host{User: "u", Host: "h", Harnesses: map[string]Harness{"claude": {Directories: []string{root}}}}, root)
	if len(inv.Traces) != 1 || len(inv.Traces[0].Members) != 2 {
		t.Fatal("subagent companion lost or treated as session", inv)
	}
}

func TestMultipleNativeFilesShareOneTrace(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"sessions/page-one.jsonl", "archived_sessions/page-two.jsonl"} {
		put(t, filepath.Join(root, p), "{\"type\":\"session_meta\",\"payload\":{\"id\":\"same-session\",\"cwd\":\"/project\"}}\n")
	}
	inv := Discover(context.Background(), Host{User: "u", Host: "h", Harnesses: map[string]Harness{"codex": {Directories: []string{root}}}}, root)
	if len(inv.Traces) != 1 || len(inv.Traces[0].Members) != 2 {
		t.Fatal("native files were lost or duplicated as traces", len(inv.Traces))
	}
}
