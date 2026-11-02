package archive

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestCLIArgumentsAndVerify(t *testing.T) {
	var out, errout bytes.Buffer
	if n := Run(context.Background(), []string{"--help"}, &out, &errout); n != 0 {
		t.Fatal(n, errout.String())
	}
	if n := Run(context.Background(), []string{"nonsense"}, &out, &errout); n != 2 {
		t.Fatal(n)
	}
	d := t.TempDir()
	cfg := filepath.Join(d, "config.toml")
	put(t, cfg, "archive=\""+filepath.Join(d, "archive")+"\"\n")
	if n := Run(context.Background(), []string{"verify", "--config", cfg}, &out, &errout); n != 0 {
		t.Fatal(n, errout.String())
	}
	if n := Run(context.Background(), []string{"pull", "missing", "--config", cfg}, &out, &errout); n != 2 {
		t.Fatal(n)
	}
}
