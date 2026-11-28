package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
)

// markdownTypographySource covers every typographic role the roadmap's D08 names:
// body, H1-H6 and fenced code, all with CJK and Latin text in the same line.
const markdownTypographySource = "# 一级标题 One\n\n## 二级标题 Two\n\n### 三级标题 Three\n\n#### 四级标题 Four\n\n##### 五级标题 Five\n\n###### 六级标题 Six\n\n正文段落包含中文与 Latin words 混排，用于校验同一实测度量。\n\n```go\nfunc 主() { println(\"你好\") }\n```\n"

// markdownTypographyTextRoles is the order of the document's blocks that carry a
// text role: six headings, one body paragraph, one fenced code block.
type markdownTypographyTextRole struct {
	name    string
	level   int // 0 for the body paragraph
	block   int
	needle  string // CJK needle whose start is a click boundary
	heading bool
}

var markdownTypographyTextRoles = []markdownTypographyTextRole{
	{name: "h1", level: 1, block: 0, needle: "标", heading: true},
	{name: "h2", level: 2, block: 1, needle: "标", heading: true},
	{name: "h3", level: 3, block: 2, needle: "标", heading: true},
	{name: "h4", level: 4, block: 3, needle: "标", heading: true},
	{name: "h5", level: 5, block: 4, needle: "标", heading: true},
	{name: "h6", level: 6, block: 5, needle: "标", heading: true},
	{name: "body", block: 6, needle: "中"},
}

const markdownTypographyCodeBlock = 7

// typographySession opens the shared role document in rendered mode with the
// platform's own text engine, so every advance is the shaped line the platform
// draws rather than reference metrics. Without that engine the roles cannot be
// measured for real, so the caller skips.
func typographySession(t *testing.T, night bool, viewport int) (*model, *app.Driver, *gfx.Canvas) {
	t.Helper()
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	state := newModel()
	state.sidebarWidth = 270
	state.dark = night
	state.session.Current().Replace(markdownTypographySource)
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	if err := driver.Resize(viewport, 955); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	return state, driver, &canvas
}

func typographyNear(got, want float32) bool {
	return math.Abs(float64(got-want)) <= 0.01
}

// typographyBlockTexts returns the text runs the last paint put inside one
// rendered block. Reading and active editing paint through the same preview
// rectangle clip, so the filter is the same for both stages.
func typographyBlockTexts(snapshot app.Snapshot, state *model, index int) []gfx.Text {
	if index < 0 || index >= len(state.preview.blocks) {
		return nil
	}
	block := &state.preview.blocks[index]
	top := state.previewRect.Y + 46 - state.previewScroll + block.top
	bottom := top + block.height + 0.01
	if block.kind == markdown.Code {
		top += codeHeaderHeight
	}
	var texts []gfx.Text
	for _, text := range snapshot.Texts {
		if text.Value == "" || text.Style.Color.Alpha <= 0 || text.Bounds.X < state.previewRect.X-1 {
			continue
		}
		// The reading view clips runs to the preview pane; the active code editor
		// clips its runs to its own row. The Y band of the block identifies both.
		if text.Bounds.Y < top-0.01 || text.Bounds.Y >= bottom {
			continue
		}
		texts = append(texts, text)
	}
	return texts
}

// TestMarkdownTypographyRolesShareMeasuredMetrics asserts that reading layout,
// active-block editing layout and click geometry share one measured style per
// role (body, H1-H6, code) at the same width, for light and dark themes and with
// CJK text. It fails when a role is rendered at one size but edited or hit-tested
// at another, or when a measured advance disagrees with the engine that draws it.
func TestMarkdownTypographyRolesShareMeasuredMetrics(t *testing.T) {
	for _, scenario := range []struct {
		night bool
		size  int
	}{{false, 16}, {true, 16}, {false, 20}, {true, 20}} {
		night := scenario.night
		name := "light"
		if night {
			name = "dark"
		}
		t.Run(fmt.Sprintf("%s/%d", name, scenario.size), func(t *testing.T) {
			state, driver, canvas := typographySession(t, night, 1780)
			state.preferences.ReadingSize = scenario.size
			if err := driver.Resize(1780, 1150); err != nil {
				t.Fatal(err)
			}
			source := state.session.Current().Text()
			for _, role := range markdownTypographyTextRoles {
				role := role
				t.Run(role.name, func(t *testing.T) {
					size, lineHeight, weight := float32(scenario.size), float32(scenario.size)*26/16, gfx.WeightRegular
					if role.heading {
						size, lineHeight, _ = markdownHeadingMetrics(role.level, night)
						size *= float32(scenario.size) / 16
						lineHeight = float32(math.Round(float64(lineHeight * float32(scenario.size) / 16)))
						weight = gfx.WeightSemibold
						if role.level == 1 {
							weight = gfx.WeightBold
						}
					}

					// Reading layout: the visible block is laid out at the role's
					// configured size, weight and line height, and each painted run's
					// advance is the engine's measurement of exactly that run.
					reading := typographyBlockTexts(driver.Snapshot(), state, role.block)
					if len(reading) == 0 {
						t.Fatalf("reading block %d painted no text", role.block)
					}
					for _, text := range reading {
						if !typographyNear(text.Style.Size, size) || !typographyNear(text.Style.Weight, weight) {
							t.Fatalf("reading run %q style=%+v, want size=%v weight=%v", text.Value, text.Style, size, weight)
						}
						advance := canvas.TextWidthStyle(text.Value, text.Style)
						if advance <= 0 || !typographyNear(text.Bounds.Width, advance) {
							t.Fatalf("reading run %q width=%v, measured advance=%v (estimated glyph width?)", text.Value, text.Bounds.Width, advance)
						}
					}
					wantHeight := lineHeight
					if role.heading && !night && role.level <= 2 {
						wantHeight += 6 // the rule and its gap
					}
					if got := state.preview.blocks[role.block].height; !typographyNear(got, wantHeight) {
						t.Fatalf("reading block height=%v, want %v", got, wantHeight)
					}
					readingWidth := state.preview.blocks[role.block].width

					// Active editing layout: activating the block must keep the same
					// measured size, weight, line height and content width.
					clickID(t, driver, renderedRows.At(role.block))
					if !state.rich.active || state.rich.index != role.block {
						t.Fatalf("block %d did not become the active editor: active=%v index=%d", role.block, state.rich.active, state.rich.index)
					}
					if !typographyNear(state.rich.wrapSize, size) || !typographyNear(state.rich.wrapWeight, weight) || !typographyNear(state.rich.wrapLineHeight, lineHeight) {
						t.Fatalf("active editor style size=%v weight=%v line=%v, reading size=%v weight=%v line=%v",
							state.rich.wrapSize, state.rich.wrapWeight, state.rich.wrapLineHeight, size, weight, lineHeight)
					}
					if !typographyNear(state.rich.wrapWidth, readingWidth) {
						t.Fatalf("active editor width=%v, reading width=%v", state.rich.wrapWidth, readingWidth)
					}
					snapshot := driver.Snapshot()
					for index, hit := range snapshot.Hits {
						if hit.ID == richInputID {
							geometry, ok := snapshot.HitTextAt(index)
							if !ok || !typographyNear(geometry.Size, size) || !typographyNear(hit.Bounds.Height, lineHeight) {
								t.Fatalf("active hit geometry=%+v, %v; want size=%v height=%v", hit.Bounds, geometry.Size, size, lineHeight)
							}
						}
					}
					active := typographyBlockTexts(driver.Snapshot(), state, role.block)
					if len(active) == 0 {
						t.Fatalf("active block %d painted no text", role.block)
					}
					for _, text := range active {
						if text.Style.Mono {
							continue // the fenced block paints through its own editor role
						}
						if !typographyNear(text.Style.Size, size) || !typographyNear(text.Style.Weight, weight) {
							t.Fatalf("active run %q style=%+v, want size=%v weight=%v", text.Value, text.Style, size, weight)
						}
					}

					// Click geometry: boundaries measured by the engine land on those
					// exact byte offsets. A second formula (estimated or pre-scaled)
					// would round to a different offset.
					visible := state.rich.input.String()
					t.Logf("role=%s size=%v line=%v weight=%v reading-width=%v line-advance=%v", role.name, size, lineHeight, weight, readingWidth, canvas.TextWidthWeight(visible, size, weight))
					offset := strings.Index(visible, role.needle)
					if offset <= 0 {
						t.Fatalf("active text %q lacks needle %q after the first character", visible, role.needle)
					}
					caret := canvas.TextCaret(visible, size, weight, offset)
					next := canvas.TextCaret(visible, size, weight, offset+len(role.needle))
					if next-caret <= 1 {
						t.Fatalf("measured advance of %q is %v", role.needle, next-caret)
					}
					wantCaret := utf8.RuneCountInString(visible[:offset])
					before := state.session.Current().Text()
					for _, click := range []struct {
						x    float32
						want int
					}{
						{caret - 0.5, wantCaret},
						{caret + 0.5, wantCaret},
						{caret + (next-caret)*0.75, wantCaret + utf8.RuneCountInString(role.needle)},
					} {
						if err := driver.Click(state.rich.rect.X+click.x, state.rich.rect.Y+state.rich.rect.Height/2); err != nil {
							t.Fatal(err)
						}
						if got := state.rich.input.Caret(); got != click.want {
							t.Fatalf("click at measured x=%v caret=%d, want %d (measured boundaries %v..%v)", click.x, got, click.want, caret, next)
						}
						placed, ok := state.rich.CaretBounds()
						byteOffset := byteOffsetOfRune(visible, click.want)
						if want := state.rich.rect.X + canvas.TextCaret(visible, reading[0].Style.Size, weight, byteOffset); !ok || !typographyNear(placed.X, want) {
							t.Fatalf("caret placement=%+v, %v; want x=%v", placed, ok, want)
						}
					}
					if state.session.Current().Text() != before {
						t.Fatal("hit-testing changed document source")
					}
				})
			}
			if state.session.Current().Text() != source {
				t.Fatal("changing typography modified Markdown bytes")
			}
		})
	}
}

// TestMarkdownTypographyCodeRoleSharesCellMetrics asserts the fenced code role is
// the same measured cell grid in reading and active editing, including a CJK line
// that has to come from a fallback family: the cell width is the base face's, so
// a fallback glyph does not change the grid.
func TestMarkdownTypographyCodeRoleSharesCellMetrics(t *testing.T) {
	state, driver, canvas := typographySession(t, false, 1605)
	size := state.readingSize() - 1
	cellWidth, cellHeight := canvas.MonoCellSize(size, markdownCodeFace)
	if cellWidth <= 0 || cellHeight <= 0 {
		t.Fatalf("code cell size=%vx%v", cellWidth, cellHeight)
	}
	reading := typographyBlockTexts(driver.Snapshot(), state, markdownTypographyCodeBlock)
	if len(reading) == 0 {
		t.Fatal("reading code block painted no text")
	}
	wantCells := 0
	for _, text := range reading {
		if !text.Style.Mono || !typographyNear(text.Style.Size, size) || text.CellWidth != cellWidth || text.Style.MonoFace != markdownCodeFace {
			t.Fatalf("reading code run %q style=%+v cell=%v, want mono size=%v cell=%v face=%v", text.Value, text.Style, text.CellWidth, size, cellWidth, markdownCodeFace)
		}
		if family := gfx.MonoFamilyName(text.Style.MonoFace); family != "Courier" {
			t.Fatalf("reading code family=%q, want Courier", family)
		}
		cells := int(math.Round(float64(text.Bounds.Width / cellWidth)))
		want := typographyMonoCells(text.Value)
		if cells != want {
			t.Fatalf("reading code run %q spans %d cells, want %d (CJK fallback must stay on the base grid)", text.Value, cells, want)
		}
		wantCells += want
	}
	if wantCells != typographyMonoCells("func 主() { println(\"你好\") }") {
		t.Fatalf("reading code cells=%d", wantCells)
	}

	clickID(t, driver, renderedRows.At(markdownTypographyCodeBlock))
	if !state.codeBlock.active {
		t.Fatal("clicking the fenced block did not activate the code editor")
	}
	active := typographyBlockTexts(driver.Snapshot(), state, markdownTypographyCodeBlock)
	if len(active) == 0 {
		t.Fatal("active code block painted no text")
	}
	for _, text := range active {
		if !text.Style.Mono || !typographyNear(text.Style.Size, size) || text.CellWidth != cellWidth || text.Style.MonoFace != markdownCodeFace {
			t.Fatalf("active code run %q style=%+v cell=%v, want reading metrics size=%v cell=%v face=%v", text.Value, text.Style, text.CellWidth, size, cellWidth, markdownCodeFace)
		}
	}
}

// typographyMonoCells mirrors the fixed-pitch grid component's cell counting for
// the assertion that a fallback glyph still occupies the base face's grid.
func typographyMonoCells(s string) int {
	cells := 0
	for _, r := range s {
		switch {
		case r < 0x1100:
			cells++
		case r >= 0x1100 && r <= 0x115F, r >= 0x2E80 && r <= 0xA4CF, r >= 0xAC00 && r <= 0xD7A3,
			r >= 0xF900 && r <= 0xFAFF, r >= 0xFE30 && r <= 0xFE6F, r >= 0xFF00 && r <= 0xFF60,
			r >= 0xFFE0 && r <= 0xFFE6, r >= 0x1F300 && r <= 0x1FAFF, r >= 0x20000 && r <= 0x3FFFD:
			cells += 2
		default:
			cells++
		}
	}
	return cells
}

// TestUIRoleAdvanceUsesCanvasMeasurement asserts the UI text role (tabs, chrome,
// status line) is measured by the same canvas engine and the same interface font
// as every other role: the same UI line keeps one advance across view modes, its
// truncation decision comes from that advance, and no mode switches the interface
// font family mid-run.
func TestUIRoleAdvanceUsesCanvasMeasurement(t *testing.T) {
	state, driver, canvas := typographySession(t, false, 1200)
	generation := canvas.FontGeneration()
	collect := func() map[string]gfx.Text {
		ui := map[string]gfx.Text{}
		for _, text := range driver.Snapshot().Texts {
			if text.Value == "" || text.Style.Color.Alpha <= 0 {
				continue
			}
			if text.HasClip && text.ClipOf == state.previewRect {
				continue // document roles are covered above
			}
			key := text.Value + "\x00" + string(rune(int(text.Style.Size*100))) + "\x00" + string(rune(int(text.Style.Weight*100))) + "\x00" + string(rune(int(text.Style.MonoFace)))
			ui[key] = text
		}
		return ui
	}
	rendered := collect()
	clickID(t, driver, editModeID)
	editing := collect()
	clickID(t, driver, readModeID)
	reading := collect()
	if len(rendered) == 0 {
		t.Fatal("no UI text painted")
	}
	checked, sizes := 0, map[float32]bool{}
	for key, renderedText := range rendered {
		editingText, ok := editing[key]
		if !ok {
			continue
		}
		readingText, ok := reading[key]
		if !ok {
			continue
		}
		advance := canvas.TextWidthStyle(renderedText.Value, renderedText.Style)
		if advance <= 0 {
			t.Fatalf("UI run %q measured no advance", renderedText.Value)
		}
		for stage, text := range map[string]gfx.Text{"editing": editingText, "reading": readingText} {
			if text.Style.Size != renderedText.Style.Size || text.Style.Weight != renderedText.Style.Weight ||
				text.Style.Italic != renderedText.Style.Italic || text.Style.Mono != renderedText.Style.Mono || text.Style.MonoFace != renderedText.Style.MonoFace {
				t.Fatalf("UI run %q metrics changed in %s: %+v vs %+v", renderedText.Value, stage, text.Style, renderedText.Style)
			}
			if got := canvas.TextWidthStyle(text.Value, text.Style); got != advance {
				t.Fatalf("UI run %q advance changed in %s: %v vs %v", renderedText.Value, stage, got, advance)
			}
		}
		// UI labels sit in fixed-width containers, so their bounds are not their
		// advance; the truncation path is what must use the measured advance.
		if fitted := ellipsize(canvas, renderedText.Value, advance, renderedText.Style.Size); fitted != renderedText.Value {
			t.Fatalf("UI run %q does not fit its own measured advance %v: %q", renderedText.Value, advance, fitted)
		}
		if trimmed := ellipsize(canvas, renderedText.Value, advance/2, renderedText.Style.Size); canvas.TextWidthWeight(trimmed, renderedText.Style.Size, renderedText.Style.Weight) > advance/2 {
			t.Fatalf("UI truncation of %q exceeds half its measured advance (%q)", renderedText.Value, trimmed)
		}
		checked++
		sizes[renderedText.Style.Size] = true
	}
	if checked < 3 {
		t.Fatalf("checked only %d UI runs across view modes", checked)
	}
	if gen := canvas.FontGeneration(); gen != generation {
		t.Fatalf("UI font generation changed across view modes: %d -> %d", generation, gen)
	}
	t.Logf("UI role sizes measured: %v", sizes)
}
