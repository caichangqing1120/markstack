package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestNightMarkdownUsesIndependentHeadingScaleAndNoGithubRules(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# First\n\n## Second\n\n### Third\n\n#### Fourth\n\n##### Fifth\n\n###### Sixth\n")
	state.dark = true
	clickID(t, driver, readModeID)
	if err := driver.Resize(1605, 955); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []struct {
		text       string
		size, line float32
	}{
		{"First", 40, 44}, {"Second", 26.08, 30}, {"Third", 18.72, 24},
		{"Fourth", 17.92, 22}, {"Fifth", 15.52, 20}, {"Sixth", 14.88, 16},
	} {
		found := false
		for _, label := range driver.Snapshot().Texts {
			if label.Value != expected.text {
				continue
			}
			found = true
			if math.Abs(float64(label.Style.Size-expected.size)) > 0.01 || label.Bounds.Height != expected.line {
				t.Errorf("%s size/line=%v/%v, want %v/%v", expected.text, label.Style.Size, label.Bounds.Height, expected.size, expected.line)
			}
			color := ui.RGB(222, 222, 222)
			if expected.text == "Fourth" || expected.text == "Sixth" {
				color = ui.RGB(255, 255, 255)
			}
			if label.Style.Color != color {
				t.Errorf("%s color=%v, want %v", expected.text, label.Style.Color, color)
			}
		}
		if !found {
			t.Errorf("missing heading %q", expected.text)
		}
	}
	for index, block := range state.preview.blocks {
		if block.rule != 0 {
			t.Errorf("Night heading %d retained a Github underline", index)
		}
	}
}

func TestNightMarkdownSeparatesParagraphsWithoutChangingLightLayout(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nFirst paragraph\n\nSecond paragraph\n")
	clickID(t, driver, readModeID)
	for _, dark := range []bool{false, true, false} {
		state.dark = dark
		if err := driver.Resize(1200, 780); err != nil {
			t.Fatal(err)
		}
		first, second := state.preview.blocks[1], state.preview.blocks[2]
		wantGap := float32(16)
		wantTop := float32(0)
		if dark {
			wantGap, wantTop = 24, 80
		}
		if gap := second.top - first.top - first.height; gap != wantGap {
			t.Errorf("dark=%v paragraph gap=%v, want %v", dark, gap, wantGap)
		}
		if top := state.preview.blocks[0].top; top != wantTop {
			t.Errorf("dark=%v first H1 top=%v, want %v", dark, top, wantTop)
		}
		if dark {
			palette := referenceMarkdownColors(true).palette
			for _, label := range driver.Snapshot().Texts {
				if label.Value == "First paragraph" && label.Style.Color != palette.ink {
					t.Errorf("Night body color=%v", label.Style.Color)
				}
			}
		}
	}
}

func TestNightMarkdownHeadingEditingKeepsReaderGeometryAndSourceBytes(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	source := []byte("\xef\xbb\xbf# Title\r\n\r\nUntouched tail\r\n")
	path := filepath.Join(t.TempDir(), "Night.md")
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.openFile(path)
	state.mode, state.dark = renderedMode, true
	canvas := &gfx.Canvas{}
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	var reader gfx.Text
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "Title" {
			reader = label
		}
	}
	if reader.Value == "" || reader.Style.Size != 40 {
		t.Fatalf("Night reader heading=%+v", reader)
	}
	if err := driver.Click(reader.Bounds.X+1, reader.Bounds.Y+reader.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.rich.active || state.rich.rect.Y != reader.Bounds.Y || state.rich.wrapSize != 40 || state.rich.wrapLineHeight != 44 {
		t.Fatalf("Night input moved: reader=%+v rect=%+v size/line=%v/%v active=%v", reader.Bounds, state.rich.rect, state.rich.wrapSize, state.rich.wrapLineHeight, state.rich.active)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte("\xef\xbb\xbf# XTitle\r\n\r\nUntouched tail\r\n"); !bytes.Equal(got, want) {
		t.Fatalf("Night edit changed source bytes: %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(got, source) {
		t.Fatalf("Night Undo/Save: %q, %v", got, err)
	}
}

func TestNightMarkdownStyledHeadingPreeditKeepsItsColorAndMarkup(t *testing.T) {
	state, driver := testDriver(t)
	source := "# before *word* after\r\n\r\nTail\r\n"
	state.session.Current().Replace(source)
	state.dark = true
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.SelectText(8, 10); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if !state.rich.mixed || state.session.Current().Text() != source {
		t.Fatal("Night preedit flattened the styled heading or changed committed source")
	}
	headingColor := ui.RGB(222, 222, 222)
	foundText, foundUnderline := false, false
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "wpind" {
			foundText = true
			if !label.Style.Italic || label.Style.Size != 40 || label.Style.Color != headingColor {
				t.Errorf("Night marked run lost heading/inline style: %+v", label.Style)
			}
		}
	}
	for _, primitive := range driver.Snapshot().Primitives {
		if primitive.Bounds.Height == 1 && primitive.Bounds.Width > 0 && primitive.Color == headingColor {
			foundUnderline = true
		}
	}
	if !foundText || !foundUnderline {
		t.Errorf("Night styled preedit text/underline=%v/%v", foundText, foundUnderline)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# before *w新d* after\r\n\r\nTail\r\n" {
		t.Fatalf("Night commit changed inline markup or neighbors: %q", got)
	}
}

func TestNightMarkdownNewParagraphKeepsFollowingBlockPositionOnCommit(t *testing.T) {
	for _, dark := range []bool{false, true} {
		state, driver := testDriver(t)
		state.dark = dark
		state.session.Current().Replace("Body\n\nTail\n")
		clickID(t, driver, renderedModeID)
		clickID(t, driver, renderedRows.At(0))
		if err := driver.Key(gfx.KeyEnd, 0); err != nil {
			t.Fatal(err)
		}
		tailY := func() float32 {
			t.Helper()
			for _, label := range driver.Snapshot().Texts {
				if label.Value == "Tail" {
					return label.Bounds.Y
				}
			}
			t.Fatal("following paragraph disappeared")
			return 0
		}
		step := float32(42)
		if dark {
			step = 50
		}
		wantInput := tailY()
		want := wantInput + step
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
		if got := tailY(); got != want {
			t.Errorf("dark=%v empty paragraph moved Tail to %v, want %v", dark, got, want)
		}
		if got := state.rich.rect.Y; got != wantInput {
			t.Errorf("dark=%v empty input top=%v, want %v", dark, got, wantInput)
		}
		if err := driver.Type("Next"); err != nil {
			t.Fatal(err)
		}
		if got := tailY(); got != want {
			t.Errorf("dark=%v paragraph commit moved Tail to %v, want %v", dark, got, want)
		}
		if got := state.session.Current().Text(); got != "Body\n\nNext\n\nTail\n" {
			t.Fatalf("new paragraph changed its neighbors: %q", got)
		}
	}
}

// TestNightMarkdownDetailsMatchMeasuredGeometryAndPalette pins the measured
// list/quote/table/code details of the fixed Github-light and Night reading
// reference. It asserts the painted geometry and palette rather than pixels:
// the same 1200 and 740 fixed columns, the Night 80px H1 top and paragraph
// rhythm, list-marker and quote indents, quote-bar colour, table border/fill,
// and the code surface and its line box.
func TestNightMarkdownDetailsMatchMeasuredGeometryAndPalette(t *testing.T) {
	source := "# Title\n\n- one\n- two\n\n> Quoted line\n>\n> - nested\n\n| A | B |\n| --- | --- |\n| one | two |\n| three | four |\n| five | six |\n\n```go\nfirst line\nsecond line\n```\n"
	for _, test := range []struct {
		name                     string
		dark                     bool
		width                    int
		sidebar                  float32
		left, content            float32
		headingSize, headingLine float32
		blockTops                []float32
	}{
		{"github-reference", false, 1200, 200, 300, 800, 36, 43, []float32{0, 65, 97, 139, 181, 223, 391}},
		{"night-reference", true, 1200, 200, 273, 854, 40, 44, []float32{80, 148, 182, 232, 282, 332, 508}},
		{"github-narrow", false, 740, 240, 270, 440, 36, 43, []float32{0, 65, 97, 139, 181, 223, 391}},
		{"night-narrow", true, 740, 240, 270, 440, 40, 44, []float32{80, 148, 182, 232, 282, 332, 508}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.sidebarWidth, state.dark = test.sidebar, test.dark
			state.session.Current().Replace(source)
			if err := driver.Resize(test.width, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, readModeID)
			if left, content := state.markdownColumn(state.previewRect); left != test.left || content != test.content {
				t.Fatalf("reading column=%v,%v want %v,%v", left, content, test.left, test.content)
			}
			if len(state.preview.blocks) != len(test.blockTops) {
				t.Fatalf("preview blocks=%d want %d", len(state.preview.blocks), len(test.blockTops))
			}
			for index, block := range state.preview.blocks {
				if block.top != test.blockTops[index] {
					t.Errorf("block %d top=%v want %v", index, block.top, test.blockTops[index])
				}
			}
			appearance := referenceMarkdownColors(test.dark)
			palette := appearance.palette
			left, content := test.left, test.content
			top := state.previewRect.Y + 46 - state.previewScroll
			snapshot := driver.Snapshot()
			text := func(value string, wantX float32) *gfx.Text {
				t.Helper()
				for index := range snapshot.Texts {
					label := &snapshot.Texts[index]
					if label.Value == value && math.Abs(float64(label.Bounds.X-wantX)) <= 0.01 {
						return label
					}
				}
				t.Fatalf("missing text %q at x=%v", value, wantX)
				return nil
			}
			heading := text("Title", left)
			headingColor := palette.ink
			if test.dark {
				headingColor = ui.RGB(222, 222, 222)
			}
			if heading.Style.Size != test.headingSize || heading.Bounds.Height != test.headingLine || heading.Style.Color != headingColor {
				t.Errorf("heading size/line/color=%v/%v/%v want %v/%v/%v", heading.Style.Size, heading.Bounds.Height, heading.Style.Color, test.headingSize, test.headingLine, headingColor)
			}
			// List rows indent text by the 24px reference marker box and keep an
			// ink marker; a quoted list keeps the muted quote ink.
			for _, item := range []struct {
				value string
				y     float32
			}{{"one", top + test.blockTops[1]}, {"two", top + test.blockTops[2]}} {
				label := text(item.value, left+24)
				if label.Style.Color != palette.ink || label.Bounds.Y != item.y {
					t.Errorf("%s ink/y=%v/%v want %v/%v", item.value, label.Style.Color, label.Bounds.Y, palette.ink, item.y)
				}
			}
			listMarkers := 0
			for _, label := range snapshot.Texts {
				if label.Value != "•" || label.Bounds.Y != top+test.blockTops[1] && label.Bounds.Y != top+test.blockTops[2] {
					continue
				}
				listMarkers++
				if label.Style.Color != palette.ink {
					t.Errorf("unquoted list marker color=%v want ink", label.Style.Color)
				}
				if right, textX := label.Bounds.X+label.Bounds.Width-2, left+24; right != textX-6 {
					t.Errorf("list marker right edge=%v want %v", right, textX-6)
				}
			}
			if listMarkers != 2 {
				t.Errorf("ordinary list markers=%d want 2", listMarkers)
			}
			bars := 0
			for _, primitive := range snapshot.Primitives {
				if primitive.Bounds.Width == 3 && primitive.Bounds.Height == 26 && primitive.Bounds.X == left+2 {
					bars++
					if primitive.Color != palette.border {
						t.Errorf("quote bar color=%v want border %v", primitive.Color, palette.border)
					}
				}
			}
			if bars != 2 {
				t.Errorf("quote bars=%d want 2", bars)
			}
			quote := text("Quoted line", left+14)
			if quote.Style.Color != palette.muted {
				t.Errorf("quote ink=%v want muted %v", quote.Style.Color, palette.muted)
			}
			nested := text("nested", left+38)
			if nested.Style.Color != palette.muted {
				t.Errorf("nested quote ink=%v want muted %v", nested.Style.Color, palette.muted)
			}
			nestedMarkers := 0
			for _, label := range snapshot.Texts {
				if label.Value != "•" || label.Bounds.Y != top+test.blockTops[4] {
					continue
				}
				nestedMarkers++
				if label.Style.Color != palette.muted {
					t.Errorf("nested marker color=%v want muted %v", label.Style.Color, palette.muted)
				}
				if right := label.Bounds.X + label.Bounds.Width - 2; right != nested.Bounds.X-6 {
					t.Errorf("nested marker right edge=%v want %v", right, nested.Bounds.X-6)
				}
			}
			if nestedMarkers != 1 {
				t.Errorf("nested list markers=%d want 1", nestedMarkers)
			}
			table := state.preview.blocks[5].table
			tableTop := top + test.blockTops[5]
			if table.width != content || len(table.columns) != 2 || len(table.rows) != 4 {
				t.Fatalf("table width/columns/rows=%v/%v/%v", table.width, table.columns, table.rows)
			}
			if table.columns[0] <= 0 || table.columns[1] <= 0 || table.columns[0]+table.columns[1] != table.width {
				t.Errorf("table columns=%v width=%v", table.columns, table.width)
			}
			horizontal, vertical := 0, 0
			for _, primitive := range snapshot.Primitives {
				line := primitive.Bounds
				if line.X < left-1 || line.X > left+table.width+1 || line.Y < tableTop-1 || line.Y > tableTop+152+1 {
					continue
				}
				if line.Width == table.width && line.Height == 1 {
					horizontal++
					if primitive.Color != palette.border {
						t.Errorf("table horizontal rule color=%v want border", primitive.Color)
					}
				}
				if line.Width == 1 && line.Height == 152 {
					vertical++
					if primitive.Color != palette.border {
						t.Errorf("table vertical rule color=%v want border", primitive.Color)
					}
				}
			}
			if horizontal != 5 || vertical != 3 {
				t.Errorf("table rules horizontal/vertical=%d/%d want 5/3", horizontal, vertical)
			}
			for row, want := range []bool{true, false, true, false} {
				found := false
				for _, primitive := range snapshot.Primitives {
					box := primitive.Bounds
					if box.X == left && box.Y == tableTop+float32(row)*38 && box.Width == table.width && box.Height == 38 && primitive.Color == palette.sidebar {
						found = true
						break
					}
				}
				if found != want {
					t.Errorf("table row %d shaded=%v want %v", row, found, want)
				}
			}
			if header := text("A", left+13); header.Style.Weight < gfx.WeightSemibold {
				t.Errorf("table header weight=%v", header.Style.Weight)
			}
			codeTop := top + test.blockTops[6]
			border, fill := false, false
			for _, primitive := range snapshot.Primitives {
				box := primitive.Bounds
				if box.X == left && box.Y == codeTop && box.Width == content && box.Height == 52 && primitive.Radius == 7.5 && primitive.Color == appearance.codeBorder {
					border = true
				}
				if box.X == left+1 && box.Y == codeTop+1 && box.Width == content-2 && box.Height == 50 && primitive.Color == appearance.codeFill {
					fill = true
				}
			}
			if !border || !fill {
				t.Errorf("code surface border/fill=%v/%v", border, fill)
			}
			first := text("first line", left+10)
			second := text("second line", left+10)
			if !first.Style.Mono || !second.Style.Mono || first.Style.Size != 15 || first.Bounds.Height != 18 {
				t.Errorf("code text mono/size/line=%v/%v/%v", first.Style.Mono, first.Style.Size, first.Bounds.Height)
			}
			if first.Bounds.Y != codeTop+8 || second.Bounds.Y != first.Bounds.Y+18 {
				t.Errorf("code line boxes=%v,%v want %v,%v", first.Bounds.Y, second.Bounds.Y, codeTop+8, codeTop+26)
			}
		})
	}
}

// TestNightMarkdownEmptyParagraphFirstCharacterStaysOnItsLine covers the
// measured native failure where the Night empty-paragraph input sat 8px too
// high and the first committed character jumped to the next line. It asserts
// the same line box for the placeholder input and its first glyph, and that
// typing more characters never reflows the paragraph or its neighbour.
func TestNightMarkdownEmptyParagraphFirstCharacterStaysOnItsLine(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			t.Run(fmt.Sprintf("dark=%v/%d", dark, width), func(t *testing.T) {
				state, driver := testDriver(t)
				state.dark = dark
				state.session.Current().Replace("Body\n\nTail\n")
				if err := driver.Resize(width, 780); err != nil {
					t.Fatal(err)
				}
				clickID(t, driver, renderedModeID)
				clickID(t, driver, renderedRows.At(0))
				if err := driver.Key(gfx.KeyEnd, 0); err != nil {
					t.Fatal(err)
				}
				textY := func(value string) (float32, float32) {
					t.Helper()
					for _, label := range driver.Snapshot().Texts {
						if label.Value == value {
							return label.Bounds.Y, label.Bounds.Height
						}
					}
					t.Fatalf("text %q not painted", value)
					return 0, 0
				}
				tailY, _ := textY("Tail")
				step := float32(42)
				if dark {
					step = 50
				}
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					t.Fatal(err)
				}
				inputY := state.rich.rect.Y
				if inputY != tailY {
					t.Fatalf("empty paragraph input top=%v want %v", inputY, tailY)
				}
				if moved, _ := textY("Tail"); moved != tailY+step {
					t.Fatalf("empty paragraph moved Tail to %v want %v", moved, tailY+step)
				}
				if err := driver.Type("Next"); err != nil {
					t.Fatal(err)
				}
				firstY, firstLine := textY("Next")
				if firstY != inputY || firstLine != 26 {
					t.Fatalf("first character line=%v/%v want %v/26", firstY, firstLine, inputY)
				}
				if state.rich.rect.Y != inputY || state.rich.wrapSize != 16 || state.rich.wrapLineHeight != 26 {
					t.Fatalf("Night body input metrics moved: rect=%+v size/line=%v/%v", state.rich.rect, state.rich.wrapSize, state.rich.wrapLineHeight)
				}
				if err := driver.Type(" continued"); err != nil {
					t.Fatal(err)
				}
				if moreY, moreLine := textY("Next continued"); moreY != inputY || moreLine != 26 {
					t.Fatalf("later characters line=%v/%v want %v/26", moreY, moreLine, inputY)
				}
				if tail, _ := textY("Tail"); tail != tailY+step {
					t.Fatalf("paragraph commit moved Tail to %v want %v", tail, tailY+step)
				}
				if got := state.session.Current().Text(); got != "Body\n\nNext continued\n\nTail\n" {
					t.Fatalf("empty paragraph changed its neighbors: %q", got)
				}
			})
		}
	}
}
