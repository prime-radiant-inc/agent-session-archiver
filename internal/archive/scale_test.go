package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHundredThousandTraces(t *testing.T) {
	if os.Getenv("ASA_SCALE_TEST") != "1" {
		t.Skip("skipped: set ASA_SCALE_TEST=1 for 100,000-file measurement")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	archive := filepath.Join(root, "archive")
	folder := filepath.Join(source, "sessions/p")
	os.MkdirAll(folder, 0700)
	started := time.Now()
	for i := 0; i < 100000; i++ {
		p := filepath.Join(folder, fmt.Sprintf("%06d.jsonl", i))
		data := fmt.Sprintf("{\"type\":\"session\",\"id\":\"s%d\",\"cwd\":\"/synthetic/project\"}\n", i)
		if e := os.WriteFile(p, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Logf("fixture creation: %s", time.Since(started))
	h := Host{User: "u", Host: "h", Harnesses: map[string]Harness{"pi": {Directories: []string{source}}}}
	started = time.Now()
	inv := Discover(context.Background(), h, root)
	elapsed := time.Since(started)
	if len(inv.Errors) > 0 || len(inv.Traces) != 100000 {
		t.Fatal(len(inv.Traces), inv.Errors)
	}
	t.Logf("discovery: %s", elapsed)
	// Materialize real manifest directories to measure the archive scan, without
	// including fsync throughput in the inventory benchmark.
	for _, m := range inv.Traces {
		v := &m.Members[0]
		sum, _, e := hashFile(v.Source)
		if e != nil {
			t.Fatal(e)
		}
		v.SHA256 = sum
		p := filepath.Join(archive, m.Dir(), "trace.json")
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e = writeBenchmarkManifest(p, m); e != nil {
			t.Fatal(e)
		}
	}
	started = time.Now()
	old, e := LoadManifests(archive)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("100,000 manifest scan: %s", time.Since(started))
	started = time.Now()
	if selected := Select(inv.Traces, old, false); len(selected) != 0 {
		t.Fatal("unchanged traces selected")
	}
	t.Logf("unchanged selection: %s", time.Since(started))
	p := filepath.Join(folder, "000000.jsonl")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("{}\n")
	f.Close()
	started = time.Now()
	next := Discover(context.Background(), h, root)
	selected := Select(next.Traces, old, false)
	t.Logf("changed discovery + selection: %s", time.Since(started))
	if len(selected) != 1 {
		t.Fatal("expected one changed trace", len(selected))
	}
	if e = Prepare(context.Background(), filepath.Join(root, "batch"), selected); e != nil {
		t.Fatal(e)
	}
}
