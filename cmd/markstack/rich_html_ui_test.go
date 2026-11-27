package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"markstack/internal/richedit"
	"markstack/internal/workspace"
)

func openRichFixture(t *testing.T) (*model, *app.Driver, string, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "richedit", "testdata", "Rich-HTML.mh"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rich.mh")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	return state, driver, path, data
}

func TestRichHTMLNativeModesShowVisibleContentWithoutChangingSource(t *testing.T) {
	state, driver, path, original := openRichFixture(t)
	if state.session.Current().Kind != workspace.RichHTMLKind || state.mode != renderedMode {
		t.Fatalf("rich file did not enter native editing: kind=%v mode=%v", state.session.Current().Kind, state.mode)
	}
	for _, value := range []string{"A &", "bold", "斜体", "标题", "引用", "本地图", "<script>"} {
		if !hasText(driver, value) {
			t.Fatalf("rich canvas omitted %q", value)
		}
	}
	if hasText(driver, `<p data-id="note">`) || state.sourceRect.Width != 0 {
		t.Fatal("rich canvas leaked supported HTML markup")
	}
	clickID(t, driver, readModeID)
	if state.mode != readMode || !hasText(driver, "bold") {
		t.Fatal("rich reading mode did not render content")
	}
	clickID(t, driver, splitModeID)
	if state.mode != readMode {
		t.Fatal("rich document entered redundant split mode")
	}
	clickID(t, driver, editModeID)
	if state.mode != editMode || state.sourceRect.Width == 0 || !hasText(driver, `<p data-id="note">`) {
		t.Fatal("explicit source inspection lost original HTML")
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) || state.session.Current().Dirty() {
		t.Fatalf("mode round-trip rewrote source: %q, %v", got, err)
	}
}

func TestRealMkDownRichFileOpensEditsAndSavesWithoutRewritingOtherHTML(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("..", "..", "internal", "richedit", "testdata", "Real-MkDown.mh"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Real-MkDown.mh")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	if state.session.Current().Kind != workspace.RichHTMLKind || state.mode != renderedMode || !hasText(driver, "真实码克样本") || !hasText(driver, "正文 A & B") {
		t.Fatal("real MkDown file did not open in native rich mode")
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("no-op save rewrote real MkDown source: %q, %v", saved, err)
	}
	clickID(t, driver, htmlLeafRows.At(1))
	if !state.html.active || driver.Snapshot().Focused != htmlInputID {
		t.Fatal("real MkDown paragraph did not activate native input")
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type(" 新"); err != nil {
		t.Fatal(err)
	}
	want := []byte("<p><strong>真实码克样本</strong></p><p>正文 A & B 新</p>")
	if state.session.Current().Text() != string(want) {
		t.Fatalf("real MkDown edit changed other source: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(original) || state.session.Current().Dirty() {
		t.Fatal("real MkDown undo did not restore original bytes")
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, want) {
		t.Fatalf("real MkDown save = %q, %v", saved, err)
	}
}

func TestRichHTMLNativeStylesAndUnsafeContent(t *testing.T) {
	_, driver, _, _ := openRichFixture(t)
	bold, italic, unsafeSource := false, false, false
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "bold" && label.Style.Weight == gfx.WeightSemibold {
			bold = true
		}
		if label.Value == "斜体" && label.Style.Italic {
			italic = true
		}
		if strings.Contains(label.Value, "<script>") {
			unsafeSource = true
		}
	}
	if !bold || !italic || !unsafeSource {
		t.Fatalf("native HTML styles: bold=%v italic=%v raw script=%v", bold, italic, unsafeSource)
	}
	for _, hit := range driver.Snapshot().Hits {
		if strings.Contains(hit.Label, "javascript:") || strings.Contains(hit.Label, "<script>") && strings.Contains(hit.Label, "打开") {
			t.Fatalf("unsafe HTML became actionable: %+v", hit)
		}
	}
}

func TestRichHTMLNativeLeafEditCommitsAfterIMEAndUndoesExactly(t *testing.T) {
	state, driver, path, original := openRichFixture(t)
	clickID(t, driver, htmlLeafRows.At(0))
	if !state.html.active || driver.Snapshot().Focused != htmlInputID {
		t.Fatalf("HTML leaf did not enter native input: active=%v focused=%v", state.html.active, driver.Snapshot().Focused)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Dirty() || state.session.Current().Text() != string(original) {
		t.Fatal("IME preedit changed HTML source")
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(original, []byte("A &amp; "), []byte("A &amp; 新"), 1)
	if state.session.Current().Text() != string(want) || !state.session.Current().Dirty() {
		t.Fatalf("IME commit changed adjacent HTML: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(original) || state.session.Current().Dirty() {
		t.Fatalf("HTML undo did not restore exact entities: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(want) {
		t.Fatalf("HTML redo = %q", state.session.Current().Text())
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("HTML save = %q, %v", got, err)
	}
}

func TestRichHTMLCompositionKeepsNativeInputFrameStable(t *testing.T) {
	state, driver, path, original := openRichFixture(t)
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	initial := driver.Snapshot().Editor
	if initial == nil || initial.Bounds.Width <= 0 || initial.Bounds.Height <= 0 {
		t.Fatal("HTML leaf has no native input frame")
	}
	initialBounds := initial.Bounds
	for _, marked := range []string{"n", "ni", strings.Repeat("candidate", 20)} {
		if err := driver.Compose(marked); err != nil {
			t.Fatal(err)
		}
		native := driver.Snapshot().Editor
		if native == nil || !native.Composing || native.Value != "A & "+marked {
			t.Fatalf("HTML native composition lost its value: %+v", native)
		}
		if native.Bounds != initialBounds {
			t.Fatalf("composition resized the native input frame: %+v -> %+v", initialBounds, native.Bounds)
		}
		if state.session.Current().Text() != string(original) || state.session.Current().Dirty() {
			t.Fatal("HTML preedit reached committed source")
		}
		if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
			t.Fatal(err)
		}
		if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
			t.Fatalf("HTML preedit changed disk: %q, %v", saved, err)
		}
	}
	if err := driver.Type("你"); err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(original, []byte("A &amp; "), []byte("A &amp; 你"), 1)
	if got := state.session.Current().Text(); got != string(want) {
		t.Fatalf("HTML composition commit changed adjacent source: %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, want) {
		t.Fatalf("HTML committed composition save=%q, %v", saved, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("HTML composition Undo/Save=%q, %v", saved, err)
	}
}

func TestRichHTMLNativeEmptyLeafRemainsEditableAfterDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.mh")
	if err := os.WriteFile(path, []byte("<p>old</p>\n<x-unknown>keep</x-unknown>"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "<p></p>\n<x-unknown>keep</x-unknown>" || !state.html.active {
		t.Fatalf("delete lost rich editing: %q active=%v", state.session.Current().Text(), state.html.active)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "<p>新</p>\n<x-unknown>keep</x-unknown>" {
		t.Fatalf("typing after delete changed unknown source: %q", state.session.Current().Text())
	}
}

func unknownHTMLBlock(t *testing.T, state *model) int {
	t.Helper()
	for index, block := range state.html.parsed.Blocks {
		if block.Kind == richedit.Unknown && strings.Contains(block.Visible, "x-unknown") {
			return index
		}
	}
	t.Fatal("unknown HTML block not parsed")
	return -1
}

func TestRichHTMLUnknownBlockOpensExplicitSourceInspection(t *testing.T) {
	state, driver, _, original := openRichFixture(t)
	index := unknownHTMLBlock(t, state)
	clickID(t, driver, htmlBlocks.At(index))
	if state.mode != editMode || state.sourceRect.Width == 0 || state.session.Current().Text() != string(original) {
		t.Fatalf("unknown block did not enter untouched source view: mode=%v", state.mode)
	}
}

func TestRichHTMLCrossReadOnlyDragRejectsPaste(t *testing.T) {
	state, driver, _, original := openRichFixture(t)
	clickID(t, driver, htmlLeafRows.At(0))
	index := unknownHTMLBlock(t, state)
	var target ui.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == htmlBlocks.At(index) {
			target = hit.Bounds
		}
	}
	if target.Width == 0 {
		t.Fatal("unknown block is not hit-testable")
	}
	active := state.html.inputRect
	if err := driver.Press(active.X+3, active.Y+active.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(target.X+10, target.Y+target.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(target.X+10, target.Y+target.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(original) || state.message == "" {
		t.Fatalf("cross-read-only paste changed HTML: %q message=%q", state.session.Current().Text(), state.message)
	}
	active = state.html.inputRect
	if err := driver.Click(active.X+3, active.Y+active.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("Y"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() == string(original) {
		t.Fatal("valid leaf input remained blocked after a new click")
	}
}

func TestRichHTMLLongDocumentScrollsWithoutEditing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.mh")
	var source strings.Builder
	for index := range 80 {
		source.WriteString("<p>Line ")
		source.WriteString(strconv.Itoa(index))
		source.WriteString("</p>\n")
	}
	if err := os.WriteFile(path, []byte(source.String()), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if state.html.height <= state.html.rect.Height {
		t.Fatal("long rich document did not create scrollable content")
	}
	if err := driver.Scroll(gfx.ScrollEvent{X: 400, Y: 350, DeltaY: 480}); err != nil {
		t.Fatal(err)
	}
	if state.html.scroll <= 0 || state.session.Current().Text() != source.String() || state.session.Current().Dirty() {
		t.Fatalf("rich scrolling changed source or stayed fixed: scroll=%v", state.html.scroll)
	}
}

func TestRichHTMLOutlineUsesVisibleHeadingsAndScrolls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outline.mh")
	var source strings.Builder
	for range 24 {
		source.WriteString("<p>Before heading</p>\n")
	}
	source.WriteString("<h2>目标标题</h2>\n<p>After heading</p>\n")
	if err := os.WriteFile(path, []byte(source.String()), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, outlineToggleID)
	if len(state.outlines) != 1 || state.outlines[0].Title != "目标标题" || state.outlines[0].Level != 2 {
		t.Fatalf("HTML outline parsed as Markdown: %+v", state.outlines)
	}
	clickID(t, driver, outlineRows.At(0))
	if state.html.scroll <= 0 || state.mode != renderedMode || state.session.Current().Text() != source.String() {
		t.Fatalf("outline did not navigate rich canvas: scroll=%v mode=%v", state.html.scroll, state.mode)
	}
}

func TestRichHTMLFindMatchesVisibleTextNotTags(t *testing.T) {
	state, driver, _, original := openRichFixture(t)
	clickID(t, driver, findDocumentID)
	if err := driver.Type("斜体"); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || len(state.findVisual) != 1 || state.findVisual[0].block != 0 || state.session.Current().Text() != string(original) {
		t.Fatalf("rich find fell back to source: mode=%v matches=%+v", state.mode, state.findVisual)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("<strong>"); err != nil {
		t.Fatal(err)
	}
	if len(state.findMatches) != 0 || state.mode != renderedMode {
		t.Fatalf("HTML markup counted as visible match: %+v mode=%v", state.findMatches, state.mode)
	}
	clickID(t, driver, readModeID)
	clickID(t, driver, findDocumentID)
	if state.mode != readMode {
		t.Fatal("find left rich reading mode")
	}
}

func TestRichHTMLFindHighlightsVisibleLeaf(t *testing.T) {
	_, driver, _, _ := openRichFixture(t)
	clickID(t, driver, findDocumentID)
	if err := driver.Type("斜体"); err != nil {
		t.Fatal(err)
	}
	var text ui.Rect
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "斜体" {
			text = label.Bounds
			break
		}
	}
	if text.Width == 0 {
		t.Fatal("visible leaf not drawn")
	}
	for _, primitive := range driver.Snapshot().Primitives {
		if primitive.Color.Alpha > 0.3 && primitive.Color.Alpha < 0.5 && primitive.Bounds.X < text.X+text.Width && primitive.Bounds.X+primitive.Bounds.Width > text.X && primitive.Bounds.Y < text.Y+text.Height && primitive.Bounds.Y+primitive.Bounds.Height > text.Y {
			return
		}
	}
	t.Fatalf("find has no highlight over visible leaf %+v", text)
}

func TestRichHTMLReadModeOpensOnlySafeLinks(t *testing.T) {
	_, driver, _, _ := openRichFixture(t)
	clickID(t, driver, readModeID)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == "打开 HTML 链接 https://example.org" {
			found = true
		}
	}
	if !found {
		t.Fatal("safe HTML link is not actionable in reading mode")
	}
	clickID(t, driver, htmlLinks.At(0))
	if opened, ok := driver.LastOpenedURL(); !ok || opened != "https://example.org" {
		t.Fatalf("HTML link opened %q, %v", opened, ok)
	}
	path := filepath.Join(t.TempDir(), "unsafe.mh")
	if err := os.WriteFile(path, []byte(`<p><a href="javascript:alert(1)">danger</a></p>`), 0644); err != nil {
		t.Fatal(err)
	}
	state, unsafeDriver := testDriver(t)
	state.openFile(path)
	if err := unsafeDriver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, unsafeDriver, readModeID)
	for _, hit := range unsafeDriver.Snapshot().Hits {
		if strings.Contains(hit.Label, "javascript:") {
			t.Fatalf("unsafe HTML link was actionable: %+v", hit)
		}
	}
	if _, ok := unsafeDriver.LastOpenedURL(); ok {
		t.Fatal("unsafe link opened a URL")
	}
}

func TestRichHTMLInlineHitRegionsDoNotOverlap(t *testing.T) {
	state, driver, _, _ := openRichFixture(t)
	check := func() {
		t.Helper()
		var hits []ui.Rect
		for _, hit := range driver.Snapshot().Hits {
			_, leaf := hit.ID.In(htmlLeafRows)
			if (leaf || hit.ID == htmlInputID) && hit.Bounds.Y < 150 {
				hits = append(hits, hit.Bounds)
			}
		}
		sort.Slice(hits, func(left, right int) bool { return hits[left].X < hits[right].X })
		if len(hits) < 4 {
			t.Fatalf("missing inline hit regions: %+v", hits)
		}
		for index := 1; index < len(hits); index++ {
			if hits[index-1].X+hits[index-1].Width > hits[index].X+0.1 {
				t.Fatalf("neighboring rich leaves overlap: %+v and %+v", hits[index-1], hits[index])
			}
		}
	}
	for _, width := range []int{1200, 740} {
		if err := driver.Resize(width, 780); err != nil {
			t.Fatal(err)
		}
		check()
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if !state.html.active {
		t.Fatal("first leaf lost active input")
	}
	check()
}

func TestRichHTMLHeadingHitCoversPaintedGlyphs(t *testing.T) {
	_, driver, _, _ := openRichFixture(t)
	var heading ui.Rect
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "标题" && label.Style.Size >= 24 {
			heading = label.Bounds
		}
	}
	if heading.Width == 0 {
		t.Fatal("HTML heading not drawn")
	}
	for _, hit := range driver.Snapshot().Hits {
		if _, leaf := hit.ID.In(htmlLeafRows); leaf && hit.Bounds.X == heading.X && hit.Bounds.Y == heading.Y {
			if hit.Bounds.X+hit.Bounds.Width < heading.X+heading.Width+0.3 {
				t.Fatalf("heading glyph spills past hit: label=%+v hit=%+v", heading, hit.Bounds)
			}
			return
		}
	}
	t.Fatal("HTML heading has no leaf hit")
}

func TestRichHTMLTableCellEditsOnlyItsSourceLeaf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.mh")
	source := "<table><tbody><tr><th>列</th><td>old</td></tr></tbody></table>\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(1))
	if !state.html.active || driver.Snapshot().Focused != htmlInputID {
		t.Fatalf("table cell not editable: active=%v focus=%v", state.html.active, driver.Snapshot().Focused)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	want := "<table><tbody><tr><th>列</th><td>oldX</td></tr></tbody></table>\n<!--keep-->"
	if state.session.Current().Text() != want {
		t.Fatalf("table edit changed unrelated HTML: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source {
		t.Fatalf("table undo did not restore source: %q", state.session.Current().Text())
	}
}

func TestRichHTMLWrappedLeafSecondRowClickInsertsAtVisibleCaret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrapped.mh")
	value := strings.Repeat("长段落中的文字", 22)
	source := "<p>" + value + "</p>\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	var rows []ui.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == htmlLeafRows.At(0) {
			rows = append(rows, hit.Bounds)
		}
	}
	sort.Slice(rows, func(left, right int) bool { return rows[left].Y < rows[right].Y })
	if len(rows) < 2 || rows[1].Y <= rows[0].Y || rows[1].Width <= 0 {
		t.Fatalf("wrapped HTML leaf has no second-row hit: %+v", rows)
	}
	if err := driver.Click(rows[1].X+min(float32(24), rows[1].Width/2), rows[1].Y+rows[1].Height/2); err != nil {
		t.Fatal(err)
	}
	caret := state.html.input.Caret()
	if !state.html.active || caret <= 0 || caret >= utf8.RuneCountInString(value) {
		t.Fatalf("second-row click placed caret at %d of %d", caret, utf8.RuneCountInString(value))
	}
	if rect, ok := state.html.CaretBounds(); !ok || rect.Y != rows[1].Y {
		t.Fatalf("second-row caret is not drawn on clicked row: %+v, %v", rect, ok)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() == source || strings.HasSuffix(state.session.Current().Text(), "新</p>\n<!--keep-->") || !strings.HasSuffix(state.session.Current().Text(), "</p>\n<!--keep-->") {
		t.Fatalf("wrapped edit missed visible caret or changed unknown tail: %q", state.session.Current().Text())
	}
}

func TestRichHTMLPlacedKeyEndMovesToVisibleRowEnd(t *testing.T) {
	state, driver, _, _ := openRichFixture(t)
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if got, want := state.html.input.Caret(), utf8.RuneCountInString("A & "); got != want {
		t.Fatalf("placed End caret=%d, want %d", got, want)
	}
}

func TestRichHTMLWrappedLeafDragSelectsAcrossVisualRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrapped-selection.mh")
	value := strings.Repeat("跨行选择文字", 25)
	source := "<p>" + value + "</p>\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	var rows []ui.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == htmlInputID {
			rows = append(rows, hit.Bounds)
		}
	}
	sort.Slice(rows, func(left, right int) bool { return rows[left].Y < rows[right].Y })
	if len(rows) < 2 {
		t.Fatalf("active HTML text has no row hits: %+v", rows)
	}
	if err := driver.Press(rows[0].X+12, rows[0].Y+rows[0].Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(rows[1].X+55, rows[1].Y+rows[1].Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(rows[1].X+55, rows[1].Y+rows[1].Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	selection := state.html.input.Selection()
	if selection.End <= selection.Start || state.html.blockedSelection {
		t.Fatalf("cross-row selection=%+v blocked=%v", selection, state.html.blockedSelection)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.session.Current().Text(), "新") || !strings.HasSuffix(state.session.Current().Text(), "</p>\n<!--keep-->") {
		t.Fatalf("selected replacement changed adjacent HTML: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source {
		t.Fatalf("cross-row undo=%q", state.session.Current().Text())
	}
}

func TestRichHTMLWrappedCJKDoesNotOverlapFollowingStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed-wrap.mh")
	source := "<p>A &amp; " + strings.Repeat("长段落测试一二三四五六七八九十", 5) + "<strong>bold</strong></p>"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	state.openFile(path)
	if err := session.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	var bold, preceding ui.Rect
	for _, label := range session.Snapshot().Texts {
		if label.Value == "bold" {
			bold = label.Bounds
		}
	}
	for _, label := range session.Snapshot().Texts {
		if label.Bounds.Y == bold.Y && label.Bounds.X < bold.X && strings.Contains(label.Value, "长段落") && label.Bounds.X > preceding.X {
			preceding = label.Bounds
		}
	}
	if bold.Width == 0 || preceding.Width == 0 {
		t.Fatalf("mixed wrapped labels missing: bold=%+v prior=%+v", bold, preceding)
	}
	for _, problem := range headless.CheckLayout(session.Snapshot()) {
		if (problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill) && strings.Contains(problem.Text, "长段落") {
			t.Fatalf("CJK glyphs overlap next styled leaf: %+v (prior=%+v bold=%+v)", problem, preceding, bold)
		}
	}
}

func TestRichHTMLCrossStyleDragPasteUndoPreservesMarkup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cross.mh")
	source := "<p>before <strong>bold</strong> after <a href=\"https://example.invalid/preserved\">link</a></p>\r\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	var bold ui.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == htmlLeafRows.At(1) {
			bold = hit.Bounds
		}
	}
	if bold.Width == 0 {
		t.Fatal("bold leaf has no drag target")
	}
	active := state.html.inputRect
	if err := driver.Press(active.X+active.Width-17, active.Y+active.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(bold.X+bold.Width/2, bold.Y+bold.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(bold.X+bold.Width/2, bold.Y+bold.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	selection := state.html.crossSelection
	if selection == nil || state.html.blockedSelection || selection.end <= selection.start || selection.blockIndex != 0 {
		t.Fatalf("cross-style selection=%+v blocked=%v", selection, state.html.blockedSelection)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil || driver.Clipboard() != selection.visible {
		t.Fatalf("cross-style copy=%q, %v; want %q", driver.Clipboard(), err, selection.visible)
	}
	block := state.html.parsed.Blocks[0]
	want, err := richedit.ReplaceSelection([]byte(source), block, selection.start, selection.end, "新")
	if err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard("新")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(want) || !strings.Contains(state.session.Current().Text(), "<strong>") || !strings.Contains(state.session.Current().Text(), `href="https://example.invalid/preserved"`) || !strings.HasSuffix(state.session.Current().Text(), "<!--keep-->") {
		t.Fatalf("cross-style paste changed untouched source: %q, want %q", state.session.Current().Text(), want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source {
		t.Fatalf("cross-style undo=%q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(want) {
		t.Fatalf("cross-style redo=%q", state.session.Current().Text())
	}
}

func TestRichHTMLCrossStyleIMEAndSubsequentUndoHistory(t *testing.T) {
	for _, ending := range []string{"insert", "unmark", "cancel-then-unmark"} {
		t.Run(ending, func(t *testing.T) { testRichHTMLCrossStyleIME(t, ending) })
	}
}

func testRichHTMLCrossStyleIME(t *testing.T, ending string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ime-cross.mh")
	source := "<p>A &amp; <strong>bold</strong> tail</p>\r\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	var bold ui.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == htmlLeafRows.At(1) {
			bold = hit.Bounds
		}
	}
	active := state.html.inputRect
	if err := driver.Press(active.X+active.Width-2, active.Y+active.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(bold.X+bold.Width/2, bold.Y+bold.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(bold.X+bold.Width/2, bold.Y+bold.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	selection := state.html.crossSelection
	if selection == nil {
		t.Fatal("IME test did not select across HTML leaves")
	}
	want, err := richedit.ReplaceSelection([]byte(source), state.html.parsed.Blocks[0], selection.start, selection.end, "拼音")
	if err != nil {
		t.Fatal(err)
	}
	selected := driver.Snapshot().Editor
	if selected == nil || selected.Selection[0] == selected.Selection[1] {
		t.Fatalf("missing native cross-style selection: %+v", selected)
	}
	marked := "pin"
	if ending != "insert" {
		marked = "拼音"
	}
	if err := driver.Compose(marked); err != nil {
		t.Fatal(err)
	}
	editor := driver.Snapshot().Editor
	if editor == nil || !editor.Composing || !strings.Contains(editor.Value, marked) {
		t.Fatalf("cross-style preedit was not kept on the native input: %+v", editor)
	}
	_, composing := state.html.input.Composition()
	if state.session.Current().Text() != source || !composing || len(state.session.RecoverySnapshot().Drafts) != 0 {
		t.Fatal("cross-style IME preedit changed disk source")
	}
	if ending == "cancel-then-unmark" {
		acknowledged, err := state.onHTMLTextEvent(app.TextEvent{Kind: app.TextCancelComposition})
		if err != nil {
			t.Fatal(err)
		}
		if acknowledged.Composing || acknowledged.Value != selected.Value || acknowledged.Selection.Start != selected.Selection[0] || acknowledged.Selection.End != selected.Selection[1] {
			t.Fatalf("cancel reply did not restore the cross-style selection: %+v", acknowledged)
		}
		state.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
		restored := driver.Snapshot().Editor
		current := state.session.Current()
		if restored == nil || restored.Composing || restored.Value != selected.Value || restored.Selection != selected.Selection || current.Text() != source || current.Dirty() || current.Editor.CanUndo() {
			t.Fatalf("cancel failed to restore the selection without a source edit: %+v, source=%q", restored, current.Text())
		}
		if err := driver.Compose(marked); err != nil {
			t.Fatal(err)
		}
	}
	if ending != "insert" {
		if err := driver.Compose(""); err != nil {
			t.Fatal(err)
		}
	} else if err := driver.Type("拼音"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(want) || !strings.Contains(state.session.Current().Text(), "&amp;") {
		t.Fatalf("IME commit rewrote untouched entity: %q", state.session.Current().Text())
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	after := state.session.Current().Text()
	if after == string(want) {
		t.Fatal("typing after cross-style commit had no effect")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(want) {
		t.Fatalf("first undo skipped single-leaf edit: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source {
		t.Fatalf("second undo skipped cross-style edit: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != after {
		t.Fatalf("redo chain lost HTML transaction: %q, want %q", state.session.Current().Text(), after)
	}
}

func TestRichHTMLCrossParagraphDragMergesSafelyAndUndoes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paragraphs.mh")
	source := "<p>First paragraph</p>\r\n<p>Second paragraph</p>\r\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	var second ui.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == htmlLeafRows.At(1) {
			second = hit.Bounds
		}
	}
	if second.Width == 0 {
		t.Fatal("second paragraph has no hit")
	}
	first := state.html.inputRect
	if err := driver.Press(first.X+first.Width/2, first.Y+first.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(second.X+second.Width/2, second.Y+second.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(second.X+second.Width/2, second.Y+second.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	selection := state.html.crossSelection
	if selection == nil || selection.lastBlock != 1 || state.html.blockedSelection || !strings.Contains(selection.visible, "\n") {
		t.Fatalf("cross-paragraph selection=%+v blocked=%v", selection, state.html.blockedSelection)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil || driver.Clipboard() != selection.visible {
		t.Fatalf("paragraph copy=%q, %v, want %q", driver.Clipboard(), err, selection.visible)
	}
	want, err := richedit.ReplaceBlockSelection([]byte(source), state.html.parsed, selection.blockIndex, selection.start, selection.lastBlock, selection.end, "新")
	if err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard("新")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(want) || !strings.HasSuffix(state.session.Current().Text(), "<!--keep-->") {
		t.Fatalf("cross-paragraph source=%q, want %q", state.session.Current().Text(), want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source {
		t.Fatalf("paragraph undo=%q", state.session.Current().Text())
	}
}

func TestRichHTMLCrossParagraphRejectsUnknownIsland(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown-island.mh")
	source := "<p>one</p><x-unknown>keep</x-unknown><p>two</p>"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	var last ui.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == htmlLeafRows.At(1) {
			last = hit.Bounds
		}
	}
	first := state.html.inputRect
	if err := driver.Press(first.X+first.Width/2, first.Y+first.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(last.X+last.Width/2, last.Y+last.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(last.X+last.Width/2, last.Y+last.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if !state.html.blockedSelection || state.html.crossSelection != nil || state.session.Current().Text() != source {
		t.Fatalf("unknown island changed source: blocked=%v selection=%+v source=%q", state.html.blockedSelection, state.html.crossSelection, state.session.Current().Text())
	}
}

func TestRichHTMLNewDocumentStartsEditableAndSavesAsMH(t *testing.T) {
	state, driver := testDriver(t)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, newRichHTMLID)
	current := state.session.Current()
	if current.Kind != workspace.RichHTMLKind || current.Path != "" || current.Text() != "<p><br></p>" || !current.Dirty() || state.mode != renderedMode || driver.Snapshot().Focused != htmlInputID {
		t.Fatalf("new rich document not ready: %+v mode=%v focus=%v", current, state.mode, driver.Snapshot().Focused)
	}
	if err := driver.Type("开始"); err != nil {
		t.Fatal(err)
	}
	if current.Text() != "<p>开始</p>" {
		t.Fatalf("new rich input=%q", current.Text())
	}
	path := filepath.Join(t.TempDir(), "created.mh")
	if err := driver.QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, saveID)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "<p>开始</p>" || current.Dirty() || current.Kind != workspace.RichHTMLKind {
		t.Fatalf("saved rich draft=%q dirty=%v err=%v", data, current.Dirty(), err)
	}
}

func TestRichHTMLOrderedListPaintsNativeNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ordered.mh")
	if err := os.WriteFile(path, []byte(`<ol start="9"><li>one</li><li>two</li></ol>`), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if !hasText(driver, "9.") || !hasText(driver, "10.") || state.session.Current().Dirty() {
		t.Fatal("ordered HTML list lost its markers or changed source")
	}
}

func TestRichHTMLParagraphEnterSplitsAndPreservesBOMCRLFHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enter.mh")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p>BodyText</p>\r\n<!--keep-->")...)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := driver.Key(gfx.KeyRight, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	want := "<p>Body</p>\r\n<p>Text</p>\r\n<!--keep-->"
	if state.session.Current().Text() != want || state.mode != renderedMode || !state.html.active || state.html.blockIndex != 1 || state.html.input.Caret() != 0 {
		t.Fatalf("rich Enter state=%q block=%d caret=%d", state.session.Current().Text(), state.html.blockIndex, state.html.input.Caret())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(original[3:]) {
		t.Fatalf("rich Enter undo=%q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != want {
		t.Fatalf("rich Enter redo=%q", state.session.Current().Text())
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, append([]byte{0xef, 0xbb, 0xbf}, []byte(want)...)) {
		t.Fatalf("rich Enter saved=%q, %v", stored, err)
	}
}

func TestRichHTMLParagraphEnterContinuesFromEmptyBlock(t *testing.T) {
	for _, source := range []string{"<p>Body</p>", "<p><br></p>"} {
		t.Run(source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "empty-enter.mh")
			if err := os.WriteFile(path, []byte(source), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			if err := driver.Resize(1200, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, htmlLeafRows.At(0))
			if err := driver.Key(gfx.KeyEnd, 0); err != nil {
				t.Fatal(err)
			}
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if err := driver.Type("新"); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(state.session.Current().Text(), "<p>新</p>") || !state.html.active || state.html.blockIndex != 1 {
				t.Fatalf("empty continuation=%q block=%d", state.session.Current().Text(), state.html.blockIndex)
			}
		})
	}
}

func TestRichHTMLParagraphEnterRejectsAmbiguousInlineMarkup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "complex-enter.mh")
	source := "<p>before<!--hidden-->after</p>"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.message == "" {
		t.Fatalf("complex Enter silently changed HTML: %q message=%q", state.session.Current().Text(), state.message)
	}
}

func TestRichHTMLParagraphBackspaceMergesAndUndoes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merge.mh")
	source := "<p>Body</p>\r\n<p>Tail</p>\r\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(1))
	if err := driver.Key(gfx.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	want := "<p>BodyTail</p>\r\n<!--keep-->"
	if state.session.Current().Text() != want || !state.html.active || state.html.blockIndex != 0 || state.html.input.Caret() != len("Body") {
		t.Fatalf("rich Backspace merge=%q block=%d caret=%d", state.session.Current().Text(), state.html.blockIndex, state.html.input.Caret())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source {
		t.Fatalf("merge undo=%q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != want {
		t.Fatalf("merge redo=%q", state.session.Current().Text())
	}
}

func TestRichHTMLBlankParagraphBackspaceReversesEnter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blank-merge.mh")
	source := "<p>Body</p>"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	blank := "<p>Body</p>\n<p><br></p>"
	if state.session.Current().Text() != blank {
		t.Fatalf("Enter did not create blank paragraph: %q", state.session.Current().Text())
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.message != "" {
		t.Fatalf("Backspace did not reverse blank paragraph: %q message=%q", state.session.Current().Text(), state.message)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != blank {
		t.Fatalf("blank merge Undo=%q", state.session.Current().Text())
	}
}

func TestRichHTMLParagraphEnterThenTypeHasOrderedUndoRedo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enter-history.mh")
	source := "<p>Body</p>\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	blank := "<p>Body</p>\n<p><br></p>\n<!--keep-->"
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := "<p>Body</p>\n<p>新</p>\n<!--keep-->"
	if state.session.Current().Text() != want {
		t.Fatalf("typed after Enter=%q", state.session.Current().Text())
	}
	for _, expected := range []string{blank, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
		if state.session.Current().Text() != expected {
			t.Fatalf("ordered Undo=%q, want %q", state.session.Current().Text(), expected)
		}
	}
	for _, expected := range []string{blank, want} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
			t.Fatal(err)
		}
		if state.session.Current().Text() != expected {
			t.Fatalf("ordered Redo=%q, want %q", state.session.Current().Text(), expected)
		}
	}
}
