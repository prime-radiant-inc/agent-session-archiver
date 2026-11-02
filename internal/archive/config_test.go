package archive

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostConfiguration(t *testing.T) {
	c, err := ParseConfig([]byte(`archive="/archive"
[[hosts]]
ssh="machine"
user="jesse"
host="machine"
[hosts.harnesses.claude]
directories=["~/.claude", "~/.claude-work", "~/.claude-personal"]
[hosts.harnesses.codex]
[[hosts]]
ssh="laptop"
user="jesse"
host="laptop"
[hosts.harnesses.pi]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Hosts) != 2 || len(c.Hosts[0].Harnesses) != 2 {
		t.Fatalf("%+v", c)
	}
	roots, err := c.Hosts[0].Roots("/home/source")
	if err != nil {
		t.Fatal(err)
	}
	if len(roots["claude"]) != 3 || roots["claude"][1] != "/home/source/.claude-work" || roots["codex"][0] != "/home/source/.codex" {
		t.Fatal(roots)
	}
	if _, ok := roots["hermes"]; ok {
		t.Fatal("unlisted harness enabled")
	}
}
func TestInvalidConfiguration(t *testing.T) {
	for _, body := range []string{
		`directories=[]`, `directories=["relative"]`, `directories=["/a", "/a/."]`,
	} {
		t.Run(body, func(t *testing.T) {
			c, err := ParseConfig([]byte("[[hosts]]\nuser=\"u\"\nhost=\"h\"\n[hosts.harnesses.claude]\n" + body))
			if err == nil {
				_, err = c.Hosts[0].Roots("/home/u")
			}
			if err == nil {
				t.Fatal("accepted invalid roots")
			}
		})
	}
	for _, body := range []string{
		"[[hosts]]\nuser=\"u\"\nhost=\"h\"\n[[hosts]]\nuser=\"u\"\nhost=\"h\"",
		"[[hosts]]\nuser=\"../u\"\nhost=\"h\"",
		"[[hosts]]\nuser=\"u\"\nhost=\"h\"\n[hosts.harnesses.hermes]",
		"archiv=\"typo\"",
	} {
		if _, err := ParseConfig([]byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return strings.TrimSpace(string(b))
}
func TestRepositoryAndIdentity(t *testing.T) {
	d := t.TempDir()
	git(t, d, "init")
	git(t, d, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	git(t, d, "remote", "add", "origin", "git@github.com:owner/repo.git")
	w := filepath.Join(t.TempDir(), "worktree")
	git(t, d, "worktree", "add", w, "-b", "other")
	if Project(d) != Project(w) {
		t.Fatal("worktrees split")
	}
	if NormalizeRemote("https://user:secret@github.com/owner/repo.git") != "github.com/owner/repo" {
		t.Fatal("remote normalization")
	}
	if Component("/a/b") == Component("/a-b") {
		t.Fatal("path collision")
	}
	if TraceID("u", "h", "codex", "/root1", "id") == TraceID("u", "h", "codex", "/root2", "id") {
		t.Fatal("root collision")
	}
	m := Manifest{User: "u", Host: "h", Harness: "codex", Root: "/root1", NativeID: "id", Project: Project(d)}
	m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, m.NativeID)
	old := m
	git(t, d, "remote", "set-url", "origin", "git@github.com:owner/renamed.git")
	m.Project = Project(d)
	selection := Select([]Manifest{m}, map[string]Manifest{old.ID: old}, false)
	if len(selection) != 0 {
		t.Fatal("unchanged trace selected")
	}
	m.Members = []Member{{Path: "session.jsonl", Source: "/root1/archived_sessions/session.jsonl", Size: 10}}
	selection = Select([]Manifest{m}, map[string]Manifest{old.ID: old}, false)
	if len(selection) != 1 || selection[0].Project != old.Project {
		t.Fatal("placement changed")
	}
	_ = os.RemoveAll(w)
}
