package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLargeCompanionInventoryProtocol(t *testing.T) {
	if os.Getenv("ASA_SCALE_TEST") != "1" {
		t.Skip("skipped: set ASA_SCALE_TEST=1 for large inventory protocol check")
	}
	root := t.TempDir()
	req := request{Op: "select", Host: Host{User: "u", Host: "h"}, Root: filepath.Join(root, "archive")}
	// Shared tool results can make modest trace counts carry large inventories.
	var members []Member
	for i := 0; i < 64; i++ {
		p := strings.Repeat("component/", 60) + fmt.Sprintf("result-%d.txt", i)
		members = append(members, Member{Path: p, Source: "/source/" + p, Size: 1})
	}
	for i := 0; i < 10000; i++ {
		m := Manifest{Version: 1, User: "u", Host: "h", Harness: "claude", Root: "/source", NativeID: fmt.Sprint(i), Project: "project", Members: members}
		m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, m.NativeID)
		req.Traces = append(req.Traces, m)
	}
	input, e := os.CreateTemp(root, "request-")
	if e != nil {
		t.Fatal(e)
	}
	defer input.Close()
	if e = json.NewEncoder(input).Encode(req); e != nil {
		t.Fatal(e)
	}
	if _, e = input.Seek(0, 0); e != nil {
		t.Fatal(e)
	}
	output, e := os.CreateTemp(root, "response-")
	if e != nil {
		t.Fatal(e)
	}
	defer output.Close()
	if e = Helper(context.Background(), input, output); e != nil {
		t.Fatal(e)
	}
	if _, e = output.Seek(0, 0); e != nil {
		t.Fatal(e)
	}
	var res response
	if e = json.NewDecoder(output).Decode(&res); e != nil {
		t.Fatal(e)
	}
	if len(res.Traces) != 10000 {
		t.Fatalf("selected %d traces", len(res.Traces))
	}
	for _, m := range res.Traces {
		if len(m.Members) != 64 {
			t.Fatal("lost companions")
		}
	}
	if _, e = os.Stat(res.Batch); e != nil {
		t.Fatal(e)
	}
}
