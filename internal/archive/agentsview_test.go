package archive

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsViewImportProof(t *testing.T) {
	if os.Getenv("ASA_AGENTSVIEW_TEST") != "1" {
		t.Skip("skipped: set ASA_AGENTSVIEW_TEST=1 for installed AgentsView import proof")
	}
	av, e := exec.LookPath("agentsview")
	if e != nil {
		t.Fatal(e)
	}
	d := t.TempDir()
	source := filepath.Join(d, "source")
	p := filepath.Join(source, "projects/example/session.jsonl")
	put(t, p, "{\"type\":\"user\",\"uuid\":\"u1\",\"sessionId\":\"archive-fixture\",\"cwd\":\"/synthetic/project\",\"timestamp\":\"2026-11-01T00:00:00Z\",\"message\":{\"role\":\"user\",\"content\":\"Synthetic archive fixture\"}}\n{\"type\":\"assistant\",\"uuid\":\"a1\",\"parentUuid\":\"u1\",\"sessionId\":\"archive-fixture\",\"cwd\":\"/synthetic/project\",\"timestamp\":\"2026-11-01T00:00:01Z\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Synthetic response\"}]}}\n")
	h := Host{User: "u", Host: "h", Harnesses: map[string]Harness{"claude": {Directories: []string{source}}}}
	inv := Discover(context.Background(), h, d)
	if len(inv.Errors) > 0 {
		t.Fatal(inv.Errors)
	}
	root := filepath.Join(d, "archive")
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
	home := filepath.Join(d, "viewer-home")
	data := filepath.Join(home, ".agentsview")
	os.MkdirAll(data, 0700)
	// Materialize one native-layout view by copying retained bytes, not by linking.
	for _, m := range inv.Traces {
		for _, v := range m.Members {
			b, e := os.ReadFile(filepath.Join(root, m.Dir(), "raw", v.Path))
			if e != nil {
				t.Fatal(e)
			}
			put(t, filepath.Join(home, ".claude", v.Path), string(b))
		}
	}
	put(t, filepath.Join(data, "config.toml"), "disabled_agents = [\"hermes\"]\n")
	cmd := exec.Command(av, "sync")
	for _, v := range os.Environ() {
		if strings.HasPrefix(v, "HOME=") || strings.HasPrefix(v, "AGENTSVIEW_") || strings.HasPrefix(v, "CLAUDE_") || strings.HasPrefix(v, "CODEX_") || strings.HasPrefix(v, "XDG_") {
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_DATA_HOME="+filepath.Join(home, ".local/share"), "XDG_STATE_HOME="+filepath.Join(home, ".local/state"), "AGENTSVIEW_NO_DAEMON=1", "AGENTSVIEW_TELEMETRY_ENABLED=0", "AGENTSVIEW_DISABLE_UPDATE_CHECK=1", "AGENTSVIEW_DATA_DIR="+data)
	output, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("AgentsView sync: %v\n%s", e, output)
	}
	b, e := exec.Command("sqlite3", "-json", filepath.Join(data, "sessions.db"), "SELECT agent, count(*) AS n FROM sessions GROUP BY agent;").Output()
	if e != nil {
		t.Fatal(e)
	}
	var rows []struct {
		Agent string `json:"agent"`
		N     int    `json:"n"`
	}
	if e = json.Unmarshal(b, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows) != 1 || rows[0].Agent != "claude" || rows[0].N != 1 {
		t.Fatalf("unexpected viewer sessions: %s", b)
	}
	if e = Verify(root); e != nil {
		t.Fatal(e)
	}
}
