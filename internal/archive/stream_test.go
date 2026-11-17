package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stageBatch captures traces into an incoming batch directory through the same
// stream a remote transfer uses.
func stageBatch(ctx context.Context, batch string, traces []Manifest) error {
	var stream bytes.Buffer
	if e := WriteBatch(ctx, &stream, traces); e != nil {
		return e
	}
	if e := os.MkdirAll(batch, 0700); e != nil {
		return e
	}
	return ReceiveBatch(&stream, batch)
}

func TestBatchStreamRoundTrip(t *testing.T) {
	d := t.TempDir()
	shared := filepath.Join(d, "shared.jsonl")
	own := filepath.Join(d, "own.jsonl")
	put(t, shared, "shared\n")
	put(t, own, "own\n")
	trace := func(id string) Manifest {
		return Manifest{Version: 1, User: "u", Host: "h", Harness: "claude", Root: d, NativeID: id, Project: "project", ID: id}
	}
	a, b := trace("a"), trace("b")
	for _, step := range []error{addMember(&a, shared, "shared.jsonl", false), addMember(&a, own, "own.jsonl", false), addMember(&b, shared, "shared.jsonl", false)} {
		if step != nil {
			t.Fatal(step)
		}
	}
	// A member the archive already holds carries its checksum and is not sent.
	a.Members[1].SHA256 = strings.Repeat("0", 64)
	var stream bytes.Buffer
	if e := WriteBatch(context.Background(), &stream, []Manifest{a, b}); e != nil {
		t.Fatal(e)
	}
	var names []string
	tr := tar.NewReader(bytes.NewReader(stream.Bytes()))
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		names = append(names, h.Name)
	}
	if len(names) != 3 || names[2] != "batch.json" {
		t.Fatalf("stream entries %v", names)
	}
	batch := filepath.Join(d, "batch")
	os.MkdirAll(batch, 0700)
	if e := ReceiveBatch(bytes.NewReader(stream.Bytes()), batch); e != nil {
		t.Fatal(e)
	}
	got, e := ReadBatch(batch)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range got {
		v := m.Members[0]
		if e = checkMember(filepath.Join(batch, m.Dir(), "raw", v.Path), v); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = os.Stat(filepath.Join(batch, a.Dir(), "raw/own.jsonl")); !os.IsNotExist(e) {
		t.Fatal("archived member was sent again")
	}
	first, _ := os.Stat(filepath.Join(batch, a.Dir(), "raw/shared.jsonl"))
	second, _ := os.Stat(filepath.Join(batch, b.Dir(), "raw/shared.jsonl"))
	if !os.SameFile(first, second) {
		t.Fatal("shared source was not received as a hard link")
	}
}

func TestReceiveBatchRejectsUnsafeStreams(t *testing.T) {
	type entry struct {
		name, link string
		kind       byte
		body       string
	}
	file := func(name, body string) entry { return entry{name: name, kind: tar.TypeReg, body: body} }
	cases := map[string][]entry{
		"parent escape":           {file("../escape", "x"), file("batch.json", "[]")},
		"absolute path":           {file("/tmp/escape", "x"), file("batch.json", "[]")},
		"unclean path":            {file("a/../b", "x"), file("batch.json", "[]")},
		"symlink":                 {{name: "link", link: "/etc/passwd", kind: tar.TypeSymlink}, file("batch.json", "[]")},
		"link to unseen entry":    {{name: "copy", link: "missing", kind: tar.TypeLink}, file("batch.json", "[]")},
		"entry after batch.json":  {file("batch.json", "[]"), file("late", "x")},
		"duplicate entry":         {file("a", "x"), file("a", "y"), file("batch.json", "[]")},
		"no batch.json":           {file("a", "x")},
		"batch.json as hard link": {file("a", "[]"), {name: "batch.json", link: "a", kind: tar.TypeLink}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			var stream bytes.Buffer
			tw := tar.NewWriter(&stream)
			for _, v := range entries {
				h := &tar.Header{Name: v.name, Linkname: v.link, Typeflag: v.kind, Mode: 0600, Size: int64(len(v.body))}
				if v.kind != tar.TypeReg {
					h.Size = 0
				}
				if e := tw.WriteHeader(h); e != nil {
					t.Fatal(e)
				}
				tw.Write([]byte(v.body))
			}
			tw.Close()
			batch := filepath.Join(t.TempDir(), "batch")
			os.MkdirAll(batch, 0700)
			if e := ReceiveBatch(&stream, batch); e == nil {
				t.Fatal("unsafe stream accepted")
			}
			if _, e := os.Stat(filepath.Join(batch, "batch.json")); !os.IsNotExist(e) {
				t.Fatal("rejected stream published batch.json")
			}
			if _, e := os.Stat(filepath.Join(filepath.Dir(batch), "escape")); !os.IsNotExist(e) {
				t.Fatal("entry escaped the batch")
			}
		})
	}
}

func TestTruncatedBatchStreamIsNotPublished(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "session.jsonl")
	put(t, src, strings.Repeat("line\n", 1000))
	m := Manifest{Version: 1, User: "u", Host: "h", Harness: "claude", Root: d, NativeID: "id", Project: "project", ID: "id"}
	if e := addMember(&m, src, "session.jsonl", false); e != nil {
		t.Fatal(e)
	}
	var stream bytes.Buffer
	if e := WriteBatch(context.Background(), &stream, []Manifest{m}); e != nil {
		t.Fatal(e)
	}
	batch := filepath.Join(d, "batch")
	os.MkdirAll(batch, 0700)
	if e := ReceiveBatch(bytes.NewReader(stream.Bytes()[:stream.Len()/2]), batch); e == nil {
		t.Fatal("truncated stream accepted")
	}
	if _, e := os.Stat(filepath.Join(batch, "batch.json")); !os.IsNotExist(e) {
		t.Fatal("truncated stream published batch.json")
	}
}

func TestWriteBatchFailsWithoutManifestWhenSourceVanishes(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "session.jsonl")
	put(t, src, "line\n")
	m := Manifest{Version: 1, User: "u", Host: "h", Harness: "claude", Root: d, NativeID: "id", Project: "project", ID: "id"}
	if e := addMember(&m, src, "session.jsonl", false); e != nil {
		t.Fatal(e)
	}
	os.Remove(src)
	var stream bytes.Buffer
	if e := WriteBatch(context.Background(), &stream, []Manifest{m}); e == nil {
		t.Fatal("missing source accepted")
	}
	if bytes.Contains(stream.Bytes(), []byte("batch.json")) {
		t.Fatal("failed capture still wrote batch.json")
	}
}
