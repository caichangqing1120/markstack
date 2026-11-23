package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewStorageFollowsActiveEditor(test *testing.T) {
	root := test.TempDir()
	large := filepath.Join(root, "large.md")
	small := filepath.Join(root, "small.md")
	for path, body := range map[string]string{large: strings.Repeat("Retained paragraph.\r\n\r\n", 1024), small: "# Small\r\n"} {
		if err := os.WriteFile(path, []byte("\xef\xbb\xbf"+body), 0o640); err != nil {
			test.Fatal(err)
		}
	}
	state, driver := testDriver(test)
	repaint := func() {
		test.Helper()
		if err := driver.Resize(740, 520); err != nil {
			test.Fatal(err)
		}
	}
	if !state.openFile(large) {
		test.Fatal(state.message)
	}
	state.mode = readMode
	repaint()
	largeCapacity := cap(state.preview.blocks)
	if largeCapacity < 1024 {
		test.Fatalf("large preview never laid out: capacity=%d", largeCapacity)
	}
	state.resetDocumentView()
	if cap(state.preview.blocks) != largeCapacity {
		test.Fatal("same-editor reset discarded preview storage")
	}
	if !state.openFile(small) {
		test.Fatal(state.message)
	}
	repaint()
	if len(state.preview.blocks) != 1 || cap(state.preview.blocks) >= largeCapacity || cap(state.parsed.Blocks) >= largeCapacity {
		test.Errorf("small tab retained previous layout: preview len/cap=%d/%d, parsed cap=%d", len(state.preview.blocks), cap(state.preview.blocks), cap(state.parsed.Blocks))
	}
	if !state.openFile(large) {
		test.Fatal(state.message)
	}
	repaint()
	if len(state.preview.blocks) != 1024 || state.session.Current().Dirty() {
		test.Fatal("returning to the large tab lost layout or changed document state")
	}
}
