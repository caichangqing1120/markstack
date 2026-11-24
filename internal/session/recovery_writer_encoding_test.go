package session

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
)

func TestRecoveryStoreWritesMarkupHeavyDraft(t *testing.T) {
	root := t.TempDir()
	store := RecoveryStore{Path: filepath.Join(root, "state", "recovery.json")}
	open := New()
	saved := []string{"anchor\r\n", strings.Repeat("<>&", (6<<20)/3) + "\r\n"}
	drafts := []string{"previous draft\r\n", "X" + saved[1][1:]}
	paths := []string{filepath.Join(root, "anchor.txt"), filepath.Join(root, "markup.txt")}
	for index, path := range paths {
		if err := os.WriteFile(path, []byte("\xef\xbb\xbf"+saved[index]), 0640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0640); err != nil {
			t.Fatal(err)
		}
		document, err := open.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		document.Replace(drafts[index])
		if err := store.Save(open.RecoverySnapshot()); err != nil {
			t.Fatalf("write draft %d: %v", index, err)
		}
	}

	loaded, err := store.Load()
	if err != nil || !reflect.DeepEqual(open.RecoverySnapshot(), loaded) {
		t.Fatalf("v1 journal did not round-trip the drafts: %v", err)
	}
	info, err := os.Stat(store.Path)
	if err != nil || info.Mode().Perm() != 0600 || info.Size() > maxRecoveryBytes {
		t.Fatalf("unexpected journal size or permissions: %v, %v", info, err)
	}
	wire, err := os.ReadFile(store.Path)
	if err != nil || !bytes.Contains(wire, []byte("<>&")) || !bytes.HasSuffix(wire, []byte("}")) {
		t.Fatalf("journal did not preserve literal markup and compact JSON: %v", err)
	}
	restored := New()
	if err := restored.RestoreRecovery(loaded); err != nil || restored.Active != 1 || len(restored.Tabs) != 2 {
		t.Fatalf("did not restore both drafts and the active tab: %v", err)
	}
	for index, draft := range restored.Tabs {
		if !draft.Dirty() || draft.Text() != drafts[index] || !loaded.Drafts[index].BOM || loaded.Drafts[index].Newline != "\r\n" {
			t.Fatalf("restored draft %d changed content or metadata", index)
		}
		data, err := os.ReadFile(paths[index])
		stat, statErr := os.Stat(paths[index])
		if err != nil || statErr != nil || string(data) != "\xef\xbb\xbf"+saved[index] || stat.Mode().Perm() != 0640 {
			t.Fatalf("source %d changed: %v, %v", index, err, statErr)
		}
	}
}

func TestRecoveryStoreLoadBoundsLargeJournalAllocation(t *testing.T) {
	store := RecoveryStore{Path: filepath.Join(t.TempDir(), "state", "recovery.json")}
	saved := strings.Repeat("a<>&\u4e2d", (6<<20)/len("a<>&\u4e2d"))
	snapshot := RecoverySnapshot{Version: 1, ActiveID: 1, Drafts: []RecoveryDraft{{
		ID: 1, Title: "large.txt", Body: "X" + saved[1:], Saved: saved, BOM: true, Newline: "\r\n",
	}}}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	runtime.GC()
	var start, end runtime.MemStats
	runtime.ReadMemStats(&start)
	data, err := readRecoveryBytes(file, info.Size())
	runtime.ReadMemStats(&end)
	if err != nil || int64(len(data)) != info.Size() {
		t.Fatalf("large journal read %d bytes, want %d: %v", len(data), info.Size(), err)
	}
	loaded, err := store.Load()
	if err != nil || !reflect.DeepEqual(loaded, snapshot) {
		t.Fatalf("large journal did not round-trip: %v", err)
	}
	allocated := end.TotalAlloc - start.TotalAlloc
	limit := uint64(info.Size()) * 5 / 4
	t.Logf("read %d journal bytes and allocated %d", info.Size(), allocated)
	if allocated > limit {
		t.Fatalf("large journal read allocated %d bytes, want at most %d", allocated, limit)
	}
}

func TestRecoveryReadDoesNotDoubleBufferForStaleSize(t *testing.T) {
	content := strings.Repeat("a", 1<<20)
	data, err := readRecoveryBytes(strings.NewReader(content), int64(len(content)-1))
	if err != nil || string(data) != content {
		t.Fatalf("read with stale size did not preserve the data: %v", err)
	}
	if cap(data) > len(content)+bytes.MinRead {
		t.Fatalf("reading one extra byte retained a %d-byte buffer for %d bytes", cap(data), len(content))
	}
}

func TestRecoveryReadHandlesChangedSize(t *testing.T) {
	content := strings.Repeat("abc", bytes.MinRead)
	for _, size := range []int64{0, int64(len(content) + bytes.MinRead)} {
		data, err := readRecoveryBytes(iotest.DataErrReader(strings.NewReader(content)), size)
		if err != nil || string(data) != content {
			t.Fatalf("read with size hint %d returned %d bytes: %v", size, len(data), err)
		}
	}
	readErr := errors.New("read failed after growth")
	if _, err := readRecoveryBytes(io.MultiReader(strings.NewReader(content), iotest.ErrReader(readErr)), 0); !errors.Is(err, readErr) {
		t.Fatalf("read error = %v, want %v", err, readErr)
	}
}

func TestRecoveryReadStopsAtWireLimitWithStaleSize(t *testing.T) {
	chunk := strings.Repeat("a", 1<<20)
	readers := make([]io.Reader, maxRecoveryBytes/len(chunk)+1)
	for index := range readers {
		readers[index] = strings.NewReader(chunk)
	}
	data, err := readRecoveryBytes(io.MultiReader(readers...), maxRecoveryBytes-bytes.MinRead)
	if err != nil || len(data) != maxRecoveryBytes+1 || cap(data) != maxRecoveryBytes+1 {
		t.Fatalf("oversized read returned len=%d cap=%d: %v", len(data), cap(data), err)
	}
}

func TestRecoveryStoreRetainsPriorJournalOnWireOverflow(t *testing.T) {
	store := RecoveryStore{Path: filepath.Join(t.TempDir(), "state", "recovery.json")}
	prior := RecoverySnapshot{Version: 1, ActiveID: 1, Drafts: []RecoveryDraft{{ID: 1, Title: "existing.txt", Body: "keep", Newline: "\n"}}}
	if err := store.Save(prior); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	control := strings.Repeat("\x01", 6<<20)
	over := RecoverySnapshot{Version: 1, ActiveID: 2, Drafts: append(prior.Drafts, RecoveryDraft{
		ID: 2, Title: "controls.txt", Body: "X" + control[1:], Saved: control, Newline: "\n",
	})}
	if err := validateRecovery(over); err != nil {
		t.Fatalf("fixture exceeded the logical cap: %v", err)
	}
	runtime.GC()
	var start, end runtime.MemStats
	runtime.ReadMemStats(&start)
	err = store.Save(over)
	runtime.ReadMemStats(&end)
	if !errors.Is(err, ErrCorruptRecovery) || !strings.Contains(err.Error(), "journal exceeds size limit") {
		t.Fatalf("expected the serialized cap to refuse the snapshot: %v", err)
	}
	after, err := os.ReadFile(store.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed write changed the previous durable journal: %v", err)
	}
	allocated := end.TotalAlloc - start.TotalAlloc
	t.Logf("allocated %d bytes while refusing the oversized snapshot", allocated)
	if allocated >= 32<<20 {
		t.Fatalf("wire-size refusal allocated %d bytes after fixture setup", allocated)
	}
}
