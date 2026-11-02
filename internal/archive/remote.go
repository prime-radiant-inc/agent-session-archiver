package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Result struct {
	Discovered int      `json:"discovered"`
	Selected   int      `json:"selected"`
	Errors     []string `json:"errors,omitempty"`
}
type HostResult struct {
	Host   string
	Result Result
	Err    error
}
type request struct {
	Op     string     `json:"op"`
	Host   Host       `json:"host"`
	Root   string     `json:"root"`
	Batch  string     `json:"batch"`
	Traces []Manifest `json:"traces"`
	Full   bool       `json:"full"`
}
type response struct {
	Inventory Inventory  `json:"inventory"`
	Traces    []Manifest `json:"traces"`
	Batch     string     `json:"batch"`
}

var sshName = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.@-]*$`)

func destination(s string) (string, string, error) {
	host, root, ok := strings.Cut(s, ":")
	if !ok || !sshName.MatchString(host) || !filepath.IsAbs(root) || strings.ContainsAny(root, "\r\n\x00") {
		return "", "", fmt.Errorf("destination must be SSH-alias:/absolute/archive")
	}
	return host, filepath.Clean(root), nil
}
func remote(ctx context.Context, host string, req request) (response, error) {
	var res response
	if !sshName.MatchString(host) {
		return res, fmt.Errorf("invalid SSH destination %q", host)
	}
	b, e := json.Marshal(req)
	if e != nil {
		return res, e
	}
	cmd := exec.CommandContext(ctx, "ssh", "-oBatchMode=yes", "-oConnectTimeout=10", host, "agent-session-archiver _helper")
	cmd.Stdin = bytes.NewReader(b)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, e := cmd.Output()
	if e != nil {
		return res, fmt.Errorf("SSH %s: %w: %s", host, e, stderr.String())
	}
	e = json.Unmarshal(raw, &res)
	return res, e
}
func Helper(ctx context.Context, in io.Reader, out io.Writer) error {
	var req request
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields()
	if e := dec.Decode(&req); e != nil {
		return e
	}
	res, e := handle(ctx, req)
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(res)
}
func handle(ctx context.Context, req request) (response, error) {
	var res response
	switch req.Op {
	case "discover":
		home, e := os.UserHomeDir()
		if e != nil {
			return res, e
		}
		res.Inventory = Discover(ctx, req.Host, home)
	case "select":
		if !filepath.IsAbs(req.Root) {
			return res, fmt.Errorf("archive root must be absolute")
		}
		if e := recoverHost(req.Root, req.Host.User, req.Host.Host); e != nil {
			return res, e
		}
		old, e := LoadManifests(req.Root)
		if e != nil {
			return res, e
		}
		res.Traces = Select(req.Traces, old, req.Full)
		if len(res.Traces) > 0 {
			res.Batch, e = NewIncoming(req.Root)
			if e != nil {
				return res, e
			}
		}
	case "recover":
		return res, recoverHost(req.Root, req.Host.User, req.Host.Host)
	case "prepare":
		batch, e := os.MkdirTemp("", "agent-session-archiver-")
		if e != nil {
			return res, e
		}
		res.Batch = batch
		if e = Prepare(ctx, batch, req.Traces); e != nil {
			return res, fmt.Errorf("prepare retained at %s: %w", batch, e)
		}
	case "incorporate":
		return res, Incorporate(req.Root, req.Batch)
	case "cleanup":
		if filepath.Dir(req.Batch) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(req.Batch), "agent-session-archiver-") {
			return res, fmt.Errorf("invalid outgoing batch")
		}
		return res, os.RemoveAll(req.Batch)
	default:
		return res, fmt.Errorf("unknown helper operation")
	}
	return res, nil
}
func transfer(ctx context.Context, source, dest string) error {
	cmd := exec.CommandContext(ctx, "rsync", "-rtH", "--protect-args", "--chmod=Du=rwx,Dgo=,Fu=rw,Fgo=", "-e", "ssh -oBatchMode=yes -oConnectTimeout=10", "--", source, dest)
	b, e := cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("rsync: %w: %s", e, b)
	}
	return nil
}
func inventoryErrors(inv Inventory) error {
	var es []error
	for _, s := range inv.Errors {
		es = append(es, errors.New(s))
	}
	return errors.Join(es...)
}
func Pull(ctx context.Context, h Host, root string, full bool) (Result, error) {
	var result Result
	// Recovery precedes contact with the source, which may no longer exist.
	if e := recoverHost(root, h.User, h.Host); e != nil {
		return result, e
	}
	discovered, e := remote(ctx, h.SSH, request{Op: "discover", Host: h})
	if e != nil {
		return result, e
	}
	inv := discovered.Inventory
	defer func() {
		for _, p := range inv.Scratch {
			remote(context.Background(), h.SSH, request{Op: "cleanup", Batch: p})
		}
	}()
	result.Discovered = len(inv.Traces)
	result.Errors = inv.Errors
	selection, e := handle(ctx, request{Op: "select", Host: h, Root: root, Traces: inv.Traces, Full: full})
	if e != nil {
		return result, e
	}
	result.Selected = len(selection.Traces)
	if result.Selected == 0 {
		return result, inventoryErrors(inv)
	}
	prepared, e := remote(ctx, h.SSH, request{Op: "prepare", Traces: selection.Traces})
	if e != nil {
		return result, e
	}
	if !filepath.IsAbs(prepared.Batch) || strings.ContainsAny(prepared.Batch, "\r\n\x00") {
		return result, fmt.Errorf("invalid remote batch")
	}
	if e = transfer(ctx, h.SSH+":"+prepared.Batch+"/", selection.Batch+"/"); e != nil {
		return result, fmt.Errorf("incoming retained at %s: %w", selection.Batch, e)
	}
	if e = Incorporate(root, selection.Batch); e != nil {
		return result, fmt.Errorf("incoming retained at %s: %w", selection.Batch, e)
	}
	if _, e = remote(ctx, h.SSH, request{Op: "cleanup", Batch: prepared.Batch}); e != nil {
		return result, e
	}
	return result, inventoryErrors(inv)
}
func Push(ctx context.Context, h Host, dest string, full bool) (Result, error) {
	var result Result
	server, root, e := destination(dest)
	if e != nil {
		return result, e
	}
	if _, e = remote(ctx, server, request{Op: "recover", Host: h, Root: root}); e != nil {
		return result, e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return result, e
	}
	inv := Discover(ctx, h, home)
	defer func() {
		for _, p := range inv.Scratch {
			handle(context.Background(), request{Op: "cleanup", Batch: p})
		}
	}()
	result.Discovered = len(inv.Traces)
	result.Errors = inv.Errors
	selection, e := remote(ctx, server, request{Op: "select", Host: h, Root: root, Traces: inv.Traces, Full: full})
	if e != nil {
		return result, e
	}
	result.Selected = len(selection.Traces)
	if result.Selected == 0 {
		return result, inventoryErrors(inv)
	}
	prepared, e := handle(ctx, request{Op: "prepare", Traces: selection.Traces})
	if e != nil {
		return result, e
	}
	if !filepath.IsAbs(selection.Batch) || strings.ContainsAny(selection.Batch, "\r\n\x00") {
		return result, fmt.Errorf("invalid remote batch")
	}
	if e = transfer(ctx, prepared.Batch+"/", server+":"+selection.Batch+"/"); e != nil {
		return result, fmt.Errorf("outgoing retained at %s, incoming at %s: %w", prepared.Batch, selection.Batch, e)
	}
	if _, e = remote(ctx, server, request{Op: "incorporate", Root: root, Batch: selection.Batch}); e != nil {
		return result, fmt.Errorf("incoming retained at %s: %w", selection.Batch, e)
	}
	if _, e = handle(ctx, request{Op: "cleanup", Batch: prepared.Batch}); e != nil {
		return result, e
	}
	return result, inventoryErrors(inv)
}
func PullHosts(ctx context.Context, hosts []Host, root string, full bool) []HostResult {
	var results []HostResult
	for _, h := range hosts {
		r, e := Pull(ctx, h, root, full)
		results = append(results, HostResult{h.Host, r, e})
		if ctx.Err() != nil {
			break
		}
	}
	return results
}
