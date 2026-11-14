package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

func TestPlainTextAlwaysStaysLiteralAndCodeOnlyStaysSource(t *testing.T) {
	root := t.TempDir()
	plain := filepath.Join(root, "literal.txt")
	htmlPath := filepath.Join(root, "page.html")
	if err := os.WriteFile(plain, []byte("# Heading\n<h1>literal</h1>\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(htmlPath, []byte(`<script>alert("never")</script>`), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	clickID(t, driver, readModeID)
	if err := driver.QueueOpenFiles([]string{plain}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	if state.session.Current().Kind != workspace.PlainKind || state.mode != editMode || state.sourceRect.Width == 0 || state.previewRect.Width != 0 || !hasText(driver, "<h1>literal</h1>") {
		t.Fatalf("plain file was rendered as Markdown: mode=%v source=%+v preview=%+v", state.mode, state.sourceRect, state.previewRect)
	}
	foundPlain := false
	for _, hit := range driver.Snapshot().Hits {
		foundPlain = foundPlain || hit.Label == "纯文本编辑器"
	}
	if !foundPlain {
		t.Fatal("plain editor lacks a literal label")
	}
	for _, id := range []gfx.ID{renderedModeID, splitModeID, readModeID} {
		clickID(t, driver, id)
		if state.mode != editMode || state.previewRect.Width != 0 {
			t.Fatalf("plain text entered rendered mode %v", state.mode)
		}
	}
	clickID(t, driver, outlineToggleID)
	if len(state.outlines) != 0 {
		t.Fatalf("plain text acquired Markdown outline: %+v", state.outlines)
	}
	if err := driver.QueueOpenFiles([]string{htmlPath}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	if state.session.Current().Kind != workspace.CodeKind || state.mode != editMode || state.sourceRect.Width == 0 || !hasText(driver, "<script>") {
		t.Fatalf("HTML code opened as executable preview: mode=%v", state.mode)
	}
	foundCode := false
	for _, hit := range driver.Snapshot().Hits {
		foundCode = foundCode || hit.Label == "代码编辑器"
	}
	if !foundCode {
		t.Fatal("code editor lacks a code label")
	}
}

func TestCodeAndPlainCanDropAndNavigateWorkspaceSearch(t *testing.T) {
	root := t.TempDir()
	code := filepath.Join(root, "main.go")
	plain := filepath.Join(root, "log.txt")
	for path, body := range map[string]string{code: "package main\n// 目标\n", plain: "plain 目标\n"} {
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace=%v, %v", changed, err)
	}
	if accepted, err := dropAt(driver, 500, 200, code, plain); err != nil || !accepted {
		t.Fatalf("code/plain drop=%v, %v", accepted, err)
	}
	if state.session.Current().Kind != workspace.PlainKind || state.mode != editMode || len(state.session.Tabs) != 3 {
		t.Fatalf("drop kinds=%+v mode=%v", state.session.Tabs, state.mode)
	}
	var hits []workspace.SearchHit
	if _, err := workspace.Search(context.Background(), root, workspace.SearchOptions{Query: "目标"}, func(hit workspace.SearchHit) bool {
		hits = append(hits, hit)
		return true
	}); err != nil || len(hits) != 2 {
		t.Fatalf("text search=%+v, %v", hits, err)
	}
	for _, hit := range hits {
		state.openWorkspaceResult(hit)
		location, err := workspace.CanonicalLocation(hit.Path)
		if err != nil {
			t.Fatal(err)
		}
		if state.mode != editMode || state.session.Current().SavedLocation() != location || state.session.Current().Editor.Selection().Start == state.session.Current().Editor.Selection().End {
			t.Fatalf("code/plain search navigation=%+v mode=%v", hit, state.mode)
		}
	}
}

func TestPlainSavePickerDefaultsToTextExtension(t *testing.T) {
	state := newModel()
	document := state.session.NewPlainText()
	if extension := saveExtension(document); extension != "txt" {
		t.Fatalf("plain Save As extension=%q", extension)
	}
}

func TestPlainExtensionsKeepMarkdownAndHTMLSymbolsLiteral(t *testing.T) {
	root := t.TempDir()
	body := "# 标题\n<h1>literal</h1>\n<script>alert(\"never\")</script>\n![图](assets/missing.png)\n"
	state, driver := testDriver(t)
	for _, name := range []string{"note.txt", "events.log", "data.csv", "data.tsv"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, readModeID)
		if !state.openFile(path) {
			t.Fatalf("%s did not open: message=%q", name, state.message)
		}
		if err := driver.Resize(1200, 780); err != nil {
			t.Fatal(err)
		}
		current := state.session.Current()
		if current.Kind != workspace.PlainKind || state.mode != editMode || state.sourceRect.Width == 0 || state.previewRect.Width != 0 {
			t.Fatalf("%s kind=%v mode=%v source=%+v preview=%+v", name, current.Kind, state.mode, state.sourceRect, state.previewRect)
		}
		if current.Text() != body || current.Dirty() {
			t.Fatalf("%s source was rewritten: %q", name, current.Text())
		}
		if !hasText(driver, "<h1>literal</h1>") || !hasText(driver, "<script>") || !hasText(driver, "# 标题") {
			t.Fatalf("%s did not paint literal source", name)
		}
		if len(state.outlines) != 0 || state.parsed.Blocks != nil {
			t.Fatalf("%s entered the Markdown reader: outlines=%+v parsed=%+v", name, state.outlines, state.parsed.Blocks)
		}
		for _, id := range []gfx.ID{renderedModeID, splitModeID, readModeID} {
			clickID(t, driver, id)
			if state.mode != editMode || state.previewRect.Width != 0 {
				t.Fatalf("%s entered rendered mode %v", name, state.mode)
			}
		}
		if err := state.session.Save(state.session.Active, ""); err != nil {
			t.Fatal(err)
		}
		if saved, err := os.ReadFile(path); err != nil || string(saved) != body {
			t.Fatalf("%s save = %q, %v", name, saved, err)
		}
	}
}

func TestHTMLAndSVGCodeStayLiteralWithoutExecutingMarkup(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, body string
	}{
		{"page.html", `<h1>hi</h1><script>alert("never")</script><img src="https://example.org/x.png">`},
		{"icon.svg", `<svg onload="never()"><text>字</text></svg>`},
	} {
		path := filepath.Join(root, test.name)
		if err := os.WriteFile(path, []byte(test.body), 0644); err != nil {
			t.Fatal(err)
		}
		state, driver := testDriver(t)
		if !state.openFile(path) {
			t.Fatalf("%s did not open: message=%q", test.name, state.message)
		}
		if err := driver.Resize(1200, 780); err != nil {
			t.Fatal(err)
		}
		if state.session.Current().Kind != workspace.CodeKind || state.mode != editMode || state.sourceRect.Width == 0 || state.previewRect.Width != 0 {
			t.Fatalf("%s kind=%v mode=%v preview=%+v", test.name, state.session.Current().Kind, state.mode, state.previewRect)
		}
		if state.session.Current().Text() != test.body || state.session.Current().Dirty() || len(state.outlines) != 0 {
			t.Fatalf("%s source was rewritten or outlined: %q %+v", test.name, state.session.Current().Text(), state.outlines)
		}
		if !hasText(driver, test.body[:min(len(test.body), 18)]) {
			t.Fatalf("%s markup was not painted as literal source", test.name)
		}
		for _, id := range []gfx.ID{renderedModeID, splitModeID, readModeID} {
			clickID(t, driver, id)
			if state.mode != editMode {
				t.Fatalf("%s executed a preview at mode %v", test.name, state.mode)
			}
		}
		if err := state.session.Save(state.session.Active, ""); err != nil {
			t.Fatal(err)
		}
		if saved, err := os.ReadFile(path); err != nil || string(saved) != test.body {
			t.Fatalf("%s save = %q, %v", test.name, saved, err)
		}
	}
}

func TestCodeAndPlainOpenAndSaveRespectSizeAndEncodingLimits(t *testing.T) {
	root := t.TempDir()
	limitPath := filepath.Join(root, "limit.txt")
	limitBody := bytes.Repeat([]byte("x"), workspace.MaxDocumentSize)
	if err := os.WriteFile(limitPath, limitBody, 0644); err != nil {
		t.Fatal(err)
	}
	state, _ := testDriver(t)
	if !state.openFile(limitPath) {
		t.Fatalf("exactly %d bytes were refused: %q", workspace.MaxDocumentSize, state.message)
	}
	current := state.session.Current()
	if current.Kind != workspace.PlainKind || len(current.Text()) != len(limitBody) || current.Dirty() {
		t.Fatalf("limit document = kind %v, %d bytes, dirty %v", current.Kind, len(current.Text()), current.Dirty())
	}
	current.Editor.SetSelection(text.Range{Start: 0, End: 1})
	current.Editor.Insert("z")
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatalf("exactly %d bytes could not be saved: %v", workspace.MaxDocumentSize, err)
	}
	saved, err := os.ReadFile(limitPath)
	if err != nil || len(saved) != workspace.MaxDocumentSize || saved[0] != 'z' {
		t.Fatalf("limit save = %d bytes, %v", len(saved), err)
	}
	current.Editor.MoveTo(current.Editor.RuneCount(), false)
	current.Editor.Insert("y")
	if current.FitsSavedSize(len(current.Text())) {
		t.Fatal("one byte over the document limit still claims to fit")
	}
	if err := state.session.Save(state.session.Active, ""); err == nil {
		t.Fatal("over-limit document was saved")
	}
	if saved, err := os.ReadFile(limitPath); err != nil || len(saved) != workspace.MaxDocumentSize || saved[0] != 'z' {
		t.Fatalf("refused save changed disk: %d bytes, %v", len(saved), err)
	}
	overPath := filepath.Join(root, "over.go")
	if err := os.WriteFile(overPath, bytes.Repeat([]byte("x"), workspace.MaxDocumentSize+1), 0644); err != nil {
		t.Fatal(err)
	}
	tabs := len(state.session.Tabs)
	if state.openFile(overPath) {
		t.Fatal("over-limit source was opened")
	}
	if len(state.session.Tabs) != tabs || state.message == "" {
		t.Fatalf("over-limit refusal tabs=%d message=%q", len(state.session.Tabs), state.message)
	}
	if info, err := os.Stat(overPath); err != nil || info.Size() != workspace.MaxDocumentSize+1 {
		t.Fatalf("over-limit source changed on disk: %+v, %v", info, err)
	}
	for _, test := range []struct {
		name string
		body []byte
	}{
		{"invalid.log", []byte{'a', 0xff, 'b'}},
		{"binary.csv", []byte{'a', 0, 'b'}},
	} {
		path := filepath.Join(root, test.name)
		if err := os.WriteFile(path, test.body, 0644); err != nil {
			t.Fatal(err)
		}
		tabs := len(state.session.Tabs)
		if state.openFile(path) {
			t.Fatalf("%s opened as an editable document", test.name)
		}
		if len(state.session.Tabs) != tabs || state.message == "" {
			t.Fatalf("%s refusal tabs=%d message=%q", test.name, len(state.session.Tabs), state.message)
		}
		if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, test.body) {
			t.Fatalf("%s changed on disk: %q, %v", test.name, saved, err)
		}
	}
}

func TestCodeHighlightBudgetBoundaryAndOverBudgetEditing(t *testing.T) {
	state := newModel()
	_, palette := state.look()
	var cache codeHighlightCache
	atLimit := strings.Repeat("x", maxCodeHighlightBytes)
	cache.Prepare(1, atLimit, "go", palette, false)
	if len(cache.lines) == 0 || cache.source != atLimit {
		t.Fatalf("exactly %d bytes were not highlighted", maxCodeHighlightBytes)
	}
	over := atLimit + "x"
	cache.Prepare(2, over, "go", palette, false)
	if len(cache.lines) != 0 || cache.source != over {
		t.Fatalf("over-budget source was tokenized: %d lines", len(cache.lines))
	}
	path := filepath.Join(t.TempDir(), "large.go")
	source := strings.Repeat("// x\n", maxCodeHighlightBytes/len("// x\n")+1)
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	view, driver := testDriver(t)
	if !view.openFile(path) {
		t.Fatalf("over-budget code did not open: %q", view.message)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if len(view.codeHighlight.lines) != 0 {
		t.Fatalf("over-budget code was highlighted in the editor: %d lines", len(view.codeHighlight.lines))
	}
	current := view.session.Current()
	if current.Kind != workspace.CodeKind || current.Text() != source || current.Dirty() {
		t.Fatalf("over-budget code = kind %v, %d bytes, dirty %v", current.Kind, len(current.Text()), current.Dirty())
	}
	clickID(t, driver, sourceID)
	current.Editor.MoveTo(current.Editor.RuneCount(), false)
	if err := driver.Type("YZ"); err != nil {
		t.Fatal(err)
	}
	want := source + "YZ"
	if current.Text() != want {
		t.Fatalf("over-budget edit = %d bytes, want %d", len(current.Text()), len(want))
	}
	if err := view.session.Save(view.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != want {
		t.Fatalf("over-budget save = %d bytes, %v", len(saved), err)
	}
}

func TestSaveExtensionMatchesOpenedDocumentKind(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, body string
		extension  string
	}{
		{"note.md", "# 标题\n", "md"},
		{"note.mh", "<p>富文本</p>", "mh"},
		{"main.go", "package main\n", "go"},
		{"literal.txt", "# literal\n", "txt"},
	} {
		path := filepath.Join(root, test.name)
		if err := os.WriteFile(path, []byte(test.body), 0644); err != nil {
			t.Fatal(err)
		}
		state, _ := testDriver(t)
		if !state.openFile(path) {
			t.Fatalf("%s did not open: %q", test.name, state.message)
		}
		if extension := saveExtension(state.session.Current()); extension != test.extension {
			t.Errorf("%s Save As extension = %q, want %q", test.name, extension, test.extension)
		}
	}
	if extension := saveExtension(newModel().session.NewDocument()); extension != "md" {
		t.Errorf("new unnamed Markdown draft extension = %q", extension)
	}
	if extension := saveExtension(newModel().session.NewPlainText()); extension != "txt" {
		t.Errorf("new unnamed plain draft extension = %q", extension)
	}
	if extension := saveExtension(newModel().session.NewRichHTML()); extension != "mh" {
		t.Errorf("new unnamed rich draft extension = %q", extension)
	}
}

func TestCodeAndPlainRecoveryRestoresKindWithoutWritingDisk(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, body, draft string
		kind              workspace.DocumentKind
	}{
		{"main.go", "package main\n", "package main\n// 草稿\n", workspace.CodeKind},
		{"literal.txt", "# 标题\n", "# 标题\n草稿\n", workspace.PlainKind},
	} {
		path := filepath.Join(root, test.name)
		if err := os.WriteFile(path, []byte(test.body), 0644); err != nil {
			t.Fatal(err)
		}
		state, _ := testDriver(t)
		if !state.openFile(path) {
			t.Fatalf("%s did not open: %q", test.name, state.message)
		}
		state.session.Current().Replace(test.draft)
		snapshot := state.session.RecoverySnapshot()
		if len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Body != test.draft {
			t.Fatalf("%s recovery snapshot = %+v", test.name, snapshot.Drafts)
		}
		restored, _ := testDriver(t)
		if err := restored.session.RestoreRecovery(snapshot); err != nil {
			t.Fatal(err)
		}
		restored.resetDocumentView()
		current := restored.session.Current()
		if current.Kind != test.kind || current.Text() != test.draft || !current.Dirty() || current.Path != path {
			t.Fatalf("%s recovered document = %+v", test.name, current)
		}
		if restored.mode != editMode {
			t.Fatalf("%s recovered into mode %v", test.name, restored.mode)
		}
		if saved, err := os.ReadFile(path); err != nil || string(saved) != test.body {
			t.Fatalf("%s recovery wrote disk early: %q, %v", test.name, saved, err)
		}
		if err := restored.session.Save(restored.session.Active, ""); err != nil {
			t.Fatal(err)
		}
		if saved, err := os.ReadFile(path); err != nil || string(saved) != test.draft {
			t.Fatalf("%s recovered save = %q, %v", test.name, saved, err)
		}
	}
}
