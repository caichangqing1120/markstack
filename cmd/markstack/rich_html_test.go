package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

func TestRichHTMLRealStructureSequentialSupportedEditsKeepOtherSource(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("..", "..", "internal", "richedit", "testdata", "Real-MkDown-Structure.mh"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Real-MkDown-Structure.mh")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	type step struct {
		row  int
		from string
		to   string
	}
	want := string(original)
	for _, edit := range []step{
		{0, "<p>结构样本</p>", "<p>结构样本新</p>"},
		{1, "<li>第一项</li>", "<li>第一项新</li>"},
		{7, ">值 & 保留</td>", ">值 & 保留新</td>"},
	} {
		clickID(t, driver, htmlLeafRows.At(edit.row))
		if !state.html.active || driver.Snapshot().Focused != htmlInputID {
			t.Fatalf("row %d did not activate a supported leaf", edit.row)
		}
		if err := driver.Key(gfx.KeyEnd, 0); err != nil {
			t.Fatal(err)
		}
		if err := driver.Type("新"); err != nil {
			t.Fatal(err)
		}
		want = strings.Replace(want, edit.from, edit.to, 1)
		if current := state.session.Current().Text(); current != want {
			t.Fatalf("row %d edit = %q, want %q", edit.row, current, want)
		}
		if !state.session.Current().Dirty() {
			t.Fatalf("row %d edit did not mark the document dirty", edit.row)
		}
	}
	if !strings.Contains(want, `<td colSpan="1" rowSpan="1" width="auto">甲</td>`) || !strings.Contains(want, "<li>第二项</li>") {
		t.Fatalf("untouched attributes or list items were rewritten: %q", want)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != want {
		t.Fatalf("saved structure sample = %q, %v", saved, err)
	}
	reopened := session.New()
	if _, err := reopened.Open(path); err != nil {
		t.Fatal(err)
	}
	if reopened.Current().Text() != want || reopened.Current().Dirty() {
		t.Fatalf("reopened structure sample = %q, want %q", reopened.Current().Text(), want)
	}
	if err := reopened.Save(reopened.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, []byte(want)) {
		t.Fatalf("reopen no-op save = %q, %v", saved, err)
	}
}

func TestRichHTMLSourceInspectionNeverUsesMarkdownParser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.mh")
	source := "<p onclick=\"never()\"># Raw HTML &amp; text</p>\n<script>never()</script>"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	clickID(t, driver, readModeID)
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Kind != workspace.RichHTMLKind || state.mode != renderedMode || len(state.parsed.Blocks) != 0 {
		t.Fatalf("rich document entered Markdown parser: mode=%v parsed=%+v", state.mode, state.parsed.Blocks)
	}
	clickID(t, driver, editModeID)
	if state.sourceRect.Width == 0 || state.previewRect.Width != 0 {
		t.Fatalf("rich source inspection unavailable: source=%+v preview=%+v", state.sourceRect, state.previewRect)
	}
	found := false
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == "HTML 原文编辑器" {
			found = true
		}
	}
	if !found || state.session.Current().Text() != source {
		t.Fatal("rich source was hidden or rewritten")
	}
	clickID(t, driver, renderedModeID)
	if err := driver.Resize(1100, 740); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || len(state.parsed.Blocks) != 0 {
		t.Fatalf("rich rendered view used Markdown: %v", state.mode)
	}
	clickID(t, driver, splitModeID)
	if state.mode != renderedMode {
		t.Fatalf("redundant rich split activated: %v", state.mode)
	}
}
