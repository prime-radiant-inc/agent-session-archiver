package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func Prepare(ctx context.Context, batch string, traces []Manifest) error {
	if err := os.MkdirAll(batch, 0700); err != nil {
		return err
	}
	for i := range traces {
		m := &traces[i]
		for j := range m.Members {
			if err := ctx.Err(); err != nil {
				return err
			}
			v := &m.Members[j]
			if v.SHA256 != "" {
				continue
			}
			if err := safeSource(m.Root, v.Source); err != nil {
				return err
			}
			f, e := os.Open(v.Source)
			if e != nil {
				return e
			}
			info, e := f.Stat()
			if e != nil {
				f.Close()
				return e
			}
			sum, e := copyOpened(f, info, filepath.Join(batch, m.Dir(), "raw", v.Path), v.Mutable)
			f.Close()
			if e != nil {
				return e
			}
			v.Size = info.Size()
			v.Mtime = info.ModTime().UnixNano()
			v.SHA256 = sum
		}
	}
	return writeJSON(filepath.Join(batch, "batch.json"), traces)
}
func copyOpened(f *os.File, info os.FileInfo, dest string, mutable bool) (string, error) {
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular source")
	}
	if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return "", e
	}
	out, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", e
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(dest)
		}
	}()
	h := sha256.New()
	if _, e = io.CopyN(io.MultiWriter(out, h), f, info.Size()); e != nil {
		return "", e
	}
	after, e := f.Stat()
	if e != nil {
		return "", e
	}
	if after.Size() < info.Size() || mutable && (after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime())) {
		return "", fmt.Errorf("source changed during capture: %s", f.Name())
	}
	if e = out.Sync(); e != nil {
		return "", e
	}
	if e = out.Close(); e != nil {
		return "", e
	}
	ok = true
	return hex.EncodeToString(h.Sum(nil)), nil
}
func ReadBatch(dir string) ([]Manifest, error) {
	var m []Manifest
	b, e := os.ReadFile(filepath.Join(dir, "batch.json"))
	if e != nil {
		return nil, e
	}
	e = json.Unmarshal(b, &m)
	return m, e
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".metadata-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(append(b, '\n')); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
