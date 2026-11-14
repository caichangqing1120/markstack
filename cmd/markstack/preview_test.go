package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
)

func nativePreview(t *testing.T, body string) (*gfx.Canvas, app.Snapshot) {
	t.Helper()
	state := newModel()
	state.session.Current().Replace(body)
	state.mode = readMode
	canvas := &gfx.Canvas{}
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return canvas, driver.Snapshot()
}

func TestNativeInlinePunctuationDoesNotOverlap(t *testing.T) {
	canvas, snapshot := nativePreview(t, "**加粗**、*斜体*和 `行内代码`。")
	var punctuation, italic *gfx.Text
	for index := range snapshot.Texts {
		label := &snapshot.Texts[index]
		if label.Value == "、" {
			punctuation = label
		}
		if label.Value == "斜体" {
			italic = label
		}
	}
	if punctuation == nil || italic == nil {
		t.Fatalf("inline pieces not present: %+v", snapshot.Texts)
	}
	right := punctuation.Bounds.X + canvas.TextWidthWeight(punctuation.Value, punctuation.Style.Size, punctuation.Style.Weight)
	if right > italic.Bounds.X+0.1 {
		t.Fatalf("shaped punctuation overlaps next span by %v px", right-italic.Bounds.X)
	}
}

func TestMarkdownInlineCodeKeepsItsReferenceFontInsideTable(t *testing.T) {
	for _, source := range []string{"`first`", "| Code |\n| --- |\n| `first` |\n"} {
		_, snapshot := nativePreview(t, source)
		found := false
		for _, label := range snapshot.Texts {
			if label.Value != "first" {
				continue
			}
			found = true
			if family := gfx.MonoFamilyName(label.Style.MonoFace); !label.Style.Mono || family != "Courier" {
				t.Errorf("inline code for %q: mono=%v family=%q", source, label.Style.Mono, family)
			}
		}
		if !found {
			t.Errorf("inline code missing for %q", source)
		}
	}
}

func TestNativeListMarkerUsesInkWithoutDarkeningQuote(t *testing.T) {
	palette := referenceMarkdownColors(false).palette
	_, snapshot := nativePreview(t, "- First\n\n> Quoted\n> - Nested\n")
	var quote *gfx.Text
	var markers []*gfx.Text
	for index := range snapshot.Texts {
		label := &snapshot.Texts[index]
		switch label.Value {
		case "•":
			markers = append(markers, label)
		case "Quoted":
			quote = label
		}
	}
	if len(markers) != 2 || quote == nil {
		t.Fatalf("list marker or quote missing: %+v", snapshot.Texts)
	}
	if markers[0].Style.Color != palette.ink {
		t.Errorf("list marker color = %+v, want ink %+v", markers[0].Style.Color, palette.ink)
	}
	if markers[1].Style.Color != palette.muted {
		t.Errorf("quoted list marker color = %+v, want muted %+v", markers[1].Style.Color, palette.muted)
	}
	if quote.Style.Color != palette.muted {
		t.Errorf("quote color = %+v, want muted %+v", quote.Style.Color, palette.muted)
	}
}

func TestTyporaHeadingScaleAndRules(t *testing.T) {
	_, snapshot := nativePreview(t, "# Heading\n\n## Section\n\n### Third\n\n#### Fourth\n\n##### Fifth\n\n###### Sixth\n\nBody")
	for _, expected := range []struct {
		text       string
		size       float32
		lineHeight float32
	}{{"Heading", 36, 43}, {"Section", 28, 34}, {"Third", 24, 34}, {"Fourth", 20, 28}, {"Fifth", 16, 22}, {"Sixth", 16, 22}} {
		found := false
		for _, label := range snapshot.Texts {
			if label.Value == expected.text {
				found = true
				if label.Style.Size < expected.size-0.1 || label.Style.Size > expected.size+0.1 {
					t.Errorf("%s size=%v, want %v", expected.text, label.Style.Size, expected.size)
				}
				if label.Bounds.Height != expected.lineHeight {
					t.Errorf("%s line=%v, want %v", expected.text, label.Bounds.Height, expected.lineHeight)
				}
			}
		}
		if !found {
			t.Errorf("missing %s", expected.text)
		}
	}
	rules := 0
	for _, primitive := range snapshot.Primitives {
		if primitive.Bounds.Width > 500 && primitive.Bounds.Height == 1 && primitive.Bounds.Y > 74 && primitive.Bounds.Y < 400 {
			rules++
		}
	}
	if rules < 2 {
		t.Errorf("heading underline rules = %d, want 2", rules)
	}
}

func TestTyporaSectionAnchorsAtMatchingViewport(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	source, err := os.ReadFile("../../examples/Welcome.md")
	if err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.session.Current().Replace(string(source))
	state.mode = renderedMode
	canvas := &gfx.Canvas{}
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Resize(1605, 729); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []struct {
		text     string
		min, max float32
	}{
		{"从这里开始", 212, 222},
		{"写作与整理", 306, 320},
		{"Markdown 示例", 529, 545},
	} {
		found := false
		for _, label := range driver.Snapshot().Texts {
			if label.Value == expected.text {
				found = true
				if label.Bounds.Y < expected.min || label.Bounds.Y > expected.max {
					t.Errorf("%s top = %v, want [%v, %v]", expected.text, label.Bounds.Y, expected.min, expected.max)
				}
			}
		}
		if !found {
			t.Errorf("missing heading %q", expected.text)
		}
	}
}

func TestNativeTableHeaderDoesNotWrapNeedlessly(t *testing.T) {
	_, snapshot := nativePreview(t, "| 操作 | 快捷键 |\n| --- | --- |\n| 新建 | ⌘N |\n| 保存 | ⌘S |")
	for _, label := range snapshot.Texts {
		if label.Value == "快捷键" {
			return
		}
	}
	t.Fatal("short Chinese header was unnecessarily split across lines")
}

func TestRenderedTableFillsReadingColumnAtWideAndNarrowWidths(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| A | B |\n| --- | --- |\n| one | two |\n")
	clickID(t, driver, renderedModeID)
	for _, size := range []struct{ width, height int }{{1200, 780}, {740, 520}} {
		if err := driver.Resize(size.width, size.height); err != nil {
			t.Fatal(err)
		}
		if len(state.preview.blocks) != 1 {
			t.Fatalf("viewport %d table blocks=%d", size.width, len(state.preview.blocks))
		}
		available := float32(800)
		if size.width == 740 {
			available = 490
		}
		table := state.preview.blocks[0].table
		if table.width < available-1 || table.width > available+1 || len(table.columns) != 2 || table.columns[0] < 64 || table.columns[1] < 64 {
			t.Errorf("viewport %d table width=%v columns=%v available=%v", size.width, table.width, table.columns, available)
		}
	}
}

func TestRenderedTableExpansionKeepsIntrinsicColumnProportions(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| 操作 | 快捷键 |\n| --- | --- |\n| 新建 | ⌘N |\n| 保存 | ⌘S |\n| 分栏 | ⌘2 |\n")
	clickID(t, driver, renderedModeID)
	if err := driver.Resize(1605, 955); err != nil {
		t.Fatal(err)
	}
	table := state.preview.blocks[0].table
	if len(table.columns) != 2 {
		t.Fatalf("columns = %v", table.columns)
	}
	share := table.columns[0] / table.width
	if share < 0.43 || share > 0.47 {
		t.Errorf("first column share = %.3f, want near the 0.44 reference split", share)
	}
}

func TestRenderedTableGridHasOuterAndColumnRules(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| A | B |\n| --- | --- |\n| one | two |\n")
	clickID(t, driver, renderedModeID)
	for _, size := range []struct{ width, height int }{{740, 520}, {1200, 780}} {
		if err := driver.Resize(size.width, size.height); err != nil {
			t.Fatal(err)
		}
		table := state.preview.blocks[0].table
		bounds := state.previewRect
		left, _ := state.markdownColumn(bounds)
		top := bounds.Y + 46 - state.previewScroll + state.preview.blocks[0].top
		height := table.rows[0] + table.rows[1]
		vertical, horizontal := 0, 0
		for _, shape := range driver.Snapshot().Primitives {
			line := shape.Bounds
			if line.X < left-1 || line.X > left+table.width+1 || line.Y < top-1 || line.Y > top+height+1 {
				continue
			}
			if line.Width == 1 && line.Height == height {
				vertical++
			}
			if line.Width == table.width && line.Height == 1 {
				horizontal++
			}
		}
		if vertical != 3 || horizontal != 3 {
			t.Errorf("%dpx table grid has %d vertical and %d horizontal rules, want 3 and 3", size.width, vertical, horizontal)
		}
	}
}

func TestRenderedTableAlternatesBodyRowFill(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| A | B |\n| --- | --- |\n| one | two |\n| three | four |\n| five | six |\n")
	clickID(t, driver, renderedModeID)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	bounds := state.previewRect
	for _, dark := range []bool{false, true} {
		if dark {
			clickID(t, driver, themeToggleID)
		}
		table := state.preview.blocks[0].table
		left, _ := state.markdownColumn(bounds)
		top := bounds.Y + 46 - state.previewScroll + state.preview.blocks[0].top
		palette := referenceMarkdownColors(dark).palette
		snapshot := driver.Snapshot()
		for row, want := range []bool{true, false, true, false} {
			found := false
			for _, shape := range snapshot.Primitives {
				box := shape.Bounds
				if box.X == left && box.Y == top && box.Width == table.width && box.Height == table.rows[row] && shape.Color == palette.sidebar {
					found = true
					break
				}
			}
			if found != want {
				t.Errorf("dark=%v table row %d shaded=%v, want %v", dark, row, found, want)
			}
			top += table.rows[row]
		}
	}
}

func TestRenderedFencedGoCodeHasNativeSyntaxColors(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n```go\npackage main\n\nfunc main() {\n    println(\"hello\")\n}\n```\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)

	var keyword, literal bool
	snapshot := driver.Snapshot()
	for _, label := range snapshot.Texts {
		for _, run := range snapshot.TextRuns(label) {
			if label.Value == "package main" && run.Color == ui.RGB(166, 38, 164) {
				keyword = true
			}
			if strings.Contains(label.Value, "println") && run.Color == ui.RGB(196, 26, 22) {
				literal = true
			}
		}
	}
	if !keyword || !literal {
		t.Fatalf("rendered Go code colors: keyword=%v literal=%v", keyword, literal)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("rendered highlighting changed source: %q", got)
	}
}

func TestTableKeepsInlineFormattingAndSafeLinks(t *testing.T) {
	_, snapshot := nativePreview(t, "| Label | Value |\n| --- | --- |\n| **Bold** | `code` |\n| [Viva](https://vivaui.dev) | Text |")
	bold, code, link := false, false, false
	for _, label := range snapshot.Texts {
		if label.Value == "Bold" && label.Style.Weight >= gfx.WeightSemibold {
			bold = true
		}
		if label.Value == "code" && label.Style.Mono {
			code = true
		}
	}
	for _, hit := range snapshot.Hits {
		if hit.Role == gfx.RoleLink && hit.Label == "Viva" {
			link = true
		}
	}
	if !bold || !code || !link {
		t.Fatalf("table semantics: bold=%v code=%v link=%v", bold, code, link)
	}
}

func TestTableLinksAfterParagraphUseCorrectDestination(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("[First](https://first.example)\n\n| Name |\n| --- |\n| [Second](https://second.example) |")
	clickID(t, driver, readModeID)
	for _, hit := range driver.Snapshot().Hits {
		if hit.Role == gfx.RoleLink && hit.Label == "Second" {
			clickID(t, driver, hit.ID)
			if target, ok := driver.LastOpenedURL(); !ok || target != "https://second.example" {
				t.Fatalf("table link opened %q, %v", target, ok)
			}
			return
		}
	}
	t.Fatal("table link missing")
}

func TestTableInlineDocumentRetainsAdjacentStylesAndLiterals(t *testing.T) {
	want := []markdown.Span{
		{Text: "A", Style: markdown.Bold}, {Text: "B", Style: markdown.Bold | markdown.Italic},
		{Text: " tail_*|` ", Style: 0}, {Text: " x`y ", Style: markdown.Mono},
		{Text: "link", Link: "https://example.com/a_(b)"},
	}
	var document markdown.Document
	setInlineDocument(&document, want, false)
	if len(document.Blocks) != 1 {
		t.Fatalf("round trip blocks = %+v", document.Blocks)
	}
	type character struct {
		value rune
		style markdown.Style
		link  string
	}
	flatten := func(spans []markdown.Span) []character {
		var result []character
		for _, span := range spans {
			for _, value := range span.Text {
				result = append(result, character{value, span.Style, span.Link})
			}
		}
		return result
	}
	got := document.Spans(&document.Blocks[0])
	if !reflect.DeepEqual(flatten(got), flatten(want)) {
		t.Fatalf("inline document got=%+v\nwant=%+v", got, want)
	}
}

// TestRenderedTableBordersAndFillsUseThemePalette pins the table decoration to
// the semantic palette in both themes and at the narrow 740px window: borders
// use the theme line colour, header and alternating body rows use the muted
// surface, and cells keep the reading ink.
func TestRenderedTableBordersAndFillsUseThemePalette(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| A | B |\n| --- | --- |\n| one | two |\n| three | four |\n| five | six |\n")
	for _, size := range []struct{ width, height int }{{740, 520}, {1200, 780}} {
		for _, dark := range []bool{false, true} {
			state.dark = dark
			if err := driver.Resize(size.width, size.height); err != nil {
				t.Fatal(err)
			}
			if state.mode != renderedMode {
				clickID(t, driver, renderedModeID)
			}
			table := state.preview.blocks[0].table
			tableHeight := float32(0)
			for _, row := range table.rows {
				tableHeight += row
			}
			left, content := state.markdownColumn(state.previewRect)
			top := state.previewRect.Y + 46 - state.previewScroll + state.preview.blocks[0].top
			palette := referenceMarkdownColors(dark).palette
			if table.width != content || len(table.rows) != 4 {
				t.Fatalf("%dpx %v table width/rows=%v/%v want %v/4", size.width, dark, table.width, table.rows, content)
			}
			horizontal, vertical := 0, 0
			for _, primitive := range driver.Snapshot().Primitives {
				line := primitive.Bounds
				if line.X < left-1 || line.X > left+table.width+1 || line.Y < top-1 || line.Y > top+tableHeight+1 {
					continue
				}
				if line.Width == table.width && line.Height == 1 {
					horizontal++
					if primitive.Color != palette.border {
						t.Errorf("%dpx dark=%v horizontal rule=%v want border %v", size.width, dark, primitive.Color, palette.border)
					}
				}
				if line.Width == 1 && line.Height == tableHeight {
					vertical++
					if primitive.Color != palette.border {
						t.Errorf("%dpx dark=%v vertical rule=%v want border %v", size.width, dark, primitive.Color, palette.border)
					}
				}
			}
			if horizontal != 5 || vertical != 3 {
				t.Errorf("%dpx dark=%v table rules=%d/%d want 5/3", size.width, dark, horizontal, vertical)
			}
			for row, want := range []bool{true, false, true, false} {
				found := false
				for _, primitive := range driver.Snapshot().Primitives {
					box := primitive.Bounds
					if box.X == left && box.Y == top+float32(row)*table.rows[row] && box.Width == table.width && box.Height == table.rows[row] && primitive.Color == palette.sidebar {
						found = true
						break
					}
				}
				if found != want {
					t.Errorf("%dpx dark=%v row %d shaded=%v want %v", size.width, dark, row, found, want)
				}
			}
			for value, want := range map[string]ui.Color{"A": palette.ink, "one": palette.ink} {
				found := false
				for _, label := range driver.Snapshot().Texts {
					if label.Value == value && label.Bounds.Y >= top && label.Bounds.Y < top+tableHeight {
						found = true
						if label.Style.Color != want {
							t.Errorf("%dpx dark=%v cell %q color=%v want %v", size.width, dark, value, label.Style.Color, want)
						}
					}
				}
				if !found {
					t.Errorf("%dpx dark=%v missing cell text %q", size.width, dark, value)
				}
			}
		}
	}
}
