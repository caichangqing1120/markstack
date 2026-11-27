package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"markstack/internal/session"
)

func TestM3StatisticsAndSourceSelection(t *testing.T) {
	state, driver := testDriver(t)
	current := state.session.Current()
	source := "中文 hello world\r\n次行 don't 😀"
	current.Replace(source)
	current.Editor.SetSelection(text.Range{Start: 0, End: 8})
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"选中 3 / 7 字", "26 字符", "2 行", "<1 分钟"} {
		if !hasText(driver, label) {
			t.Errorf("missing status %q", label)
		}
	}
	if current.Text() != source {
		t.Fatal("statistics changed the source")
	}
}

func TestM3WritingModesFollowCaret(t *testing.T) {
	state, driver := testDriver(t)
	current := state.session.Current()
	current.Replace(strings.Repeat("Context paragraph.\n\n", 36) + "Active paragraph.\n\n" + strings.Repeat("Following paragraph.\n\n", 36))
	clickID(t, driver, sourceID)
	current.Editor.MoveTo(36*20+4, false)
	for _, character := range []string{"t", "d"} {
		if err := driver.Chord(primaryModifier()|gfx.ModifierAlt, character); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	caret, ok := state.source.CaretBounds()
	if !ok || deltaAbs(caret.Y+caret.Height/2-(state.sourceRect.Y+state.sourceRect.Height/2)) > 24 {
		t.Fatalf("source caret not centered: %v in %v", caret, state.sourceRect)
	}
	if !hasText(driver, "Active paragraph.") || !state.preferences.FocusMode {
		t.Fatal("focus mode did not turn on")
	}
	if err := driver.Key(gfx.KeyDown, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("输入"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Advance(200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if caret, ok := state.source.CaretBounds(); !ok || deltaAbs(caret.Y+caret.Height/2-(state.sourceRect.Y+state.sourceRect.Height/2)) > 24 {
		t.Fatalf("source caret moved away from center after typing: %v", caret)
	}
}

func deltaAbs(value float32) float32 { return max(value, -value) }

func TestM3SourceTypewriterCentersShortDocumentEdges(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("第一行\nMiddle\nLast")
	clickID(t, driver, sourceID)
	if err := driver.Chord(primaryModifier()|gfx.ModifierAlt, "t"); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{0, state.session.Current().Editor.RuneCount()} {
		state.session.Current().Editor.MoveTo(offset, false)
		state.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
		if err := driver.Advance(200 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		caret, ok := state.source.CaretBounds()
		if !ok || deltaAbs(caret.Y+caret.Height/2-(state.sourceRect.Y+state.sourceRect.Height/2)) > 24 {
			t.Fatalf("short document edge %d not centered: %v", offset, caret)
		}
	}
}

func TestM3StatsCacheAndMixedUnicode(t *testing.T) {
	var stats documentStats
	value := "中文ABC don't cafe\u0301 한국語。🙂\r\n終\r"
	stats.update(value)
	if len(stats.words) != 9 || stats.cjk != 6 || stats.lines != 3 || stats.selected(0, 8) != 3 {
		t.Fatalf("unexpected statistics: %+v", stats)
	}
	large := strings.Repeat("hello world markdown 1234567890\n", 1<<15)
	stats.update(large)
	if allocations := testing.AllocsPerRun(100, func() {
		stats.update(large)
		if stats.selected(stats.byteAt(7), stats.byteAt(stats.characters)) != (1<<17)-1 {
			panic("incorrect selection range")
		}
	}); allocations != 0 {
		t.Fatalf("cached 1 MiB stats allocated %v", allocations)
	}
	stats.update("changed")
	if len(stats.words) != 1 || stats.characters != 7 {
		t.Fatalf("cached stats survived changed text: %+v", stats)
	}
}

func BenchmarkM3StatsCache1MiB(b *testing.B) {
	source := strings.Repeat("hello world markdown 1234567890\n", 1<<15)
	var stats documentStats
	stats.update(source)
	b.ReportAllocs()
	for b.Loop() {
		stats.update(source)
		_ = stats.selected(stats.byteAt(121), stats.byteAt(stats.characters-121))
	}
}

func TestM3OneMiBOutlineAndPreviewReuseUnchangedLayout(t *testing.T) {
	state, driver := testDriver(t)
	paragraph := strings.Repeat("Text for outline and preview cache. ", 6)
	section := "# Section\n\n" + paragraph + "\n\n"
	source := strings.Repeat(section, (1<<20)/len(section)+1)
	state.session.Current().Replace(source)
	clickID(t, driver, readModeID)
	clickID(t, driver, outlineToggleID)
	entry := &state.outlinePanel.entries[0]
	block := &state.preview.sourceBlocks[0]
	for range 6 {
		if err := driver.Scroll(gfx.ScrollEvent{X: state.previewRect.X + 40, Y: state.previewRect.Y + 80, DeltaY: 600, Precise: true}); err != nil {
			t.Fatal(err)
		}
		if entry != &state.outlinePanel.entries[0] || block != &state.preview.sourceBlocks[0] {
			t.Fatal("unchanged scroll reparsed the 1 MiB document")
		}
	}
	if state.outlinePanel.active <= 0 {
		t.Fatal("outline did not track the scrolled 1 MiB document")
	}
}

func TestM3OutlineFilterIgnoresFoldsUntilCleared(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Parent\n\n### Hidden Child\n\n#### Deep\n\n# Peer")
	clickID(t, driver, outlineToggleID)
	clickID(t, driver, outlineFolds.At(0))
	if m3HasHit(driver, outlineRows.At(1)) || m3HasHit(driver, outlineRows.At(2)) || !m3HasHit(driver, outlineRows.At(3)) {
		t.Fatal("fold did not hide every descendant")
	}
	clickID(t, driver, outlineFilterID)
	if err := driver.Type("cHiLd"); err != nil {
		t.Fatal(err)
	}
	if !m3HasHit(driver, outlineRows.At(1)) || m3HasHit(driver, outlineRows.At(0)) || m3HasHit(driver, outlineRows.At(3)) {
		t.Fatal("filter did not isolate the matching heading")
	}
	clickID(t, driver, outlineClearID)
	if m3HasHit(driver, outlineRows.At(1)) || !m3HasHit(driver, outlineRows.At(0)) {
		t.Fatal("clear did not restore the previous fold")
	}
	clickID(t, driver, outlineFolds.At(0))
	if !m3HasHit(driver, outlineRows.At(2)) {
		t.Fatal("expand did not restore nested headings")
	}
}

func m3HasHit(driver *app.Driver, id gfx.ID) bool {
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id {
			return true
		}
	}
	return false
}

func TestM3ReadLinksOpenRelativeTabsAndAnchors(t *testing.T) {
	state, driver := testDriver(t)
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	intro := "[**Jump**](#重复-1) [Other](../guide%20one.md#the-section) [Missing](./missing.md) [Escape](../../outside.md) [Bad](javascript:alert(1))\n\n"
	padding := strings.Repeat("Padding text.\n\n", 30)
	mainPath := filepath.Join(sub, "main.md")
	guide := filepath.Join(root, "guide one.md")
	for filename, contents := range map[string]string{
		mainPath: intro + "# 重复\n\n" + padding + "# 重复\n\n" + padding,
		guide:    "# Intro\n\n" + padding + "## The Section\n\n" + padding,
	} {
		if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state.session.Root = root
	if !state.openFile(mainPath) {
		t.Fatal(state.message)
	}
	clickID(t, driver, readModeID)
	clickLink := func(label string) {
		t.Helper()
		for _, hit := range driver.Snapshot().Hits {
			if hit.Role == gfx.RoleLink && hit.Label == label {
				clickID(t, driver, hit.ID)
				return
			}
		}
		t.Fatalf("no link for %q: %+v", label, state.previewLinkHits)
	}
	clickLink("Missing")
	if !hasText(driver, "找不到 Markdown 文件") || state.session.Current().Path != mainPath {
		t.Fatalf("missing link: %s", state.message)
	}
	clickLink("Escape")
	if !strings.Contains(state.message, "工作区外") || state.session.Current().Path != mainPath {
		t.Fatalf("escaping link: %s", state.message)
	}
	clickLink("Jump")
	if err := driver.Advance(200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.previewScroll < 400 || state.session.Current().Path != mainPath {
		t.Fatalf("same-document anchor scroll=%v, message=%q", state.previewScroll, state.message)
	}
	if err := driver.Scroll(gfx.ScrollEvent{X: state.previewRect.X + 40, Y: state.previewRect.Y + 90, DeltaY: -10000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	clickLink("Other")
	if err := driver.Advance(200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Path != guide || state.previewScroll < 400 || state.mode != readMode {
		t.Fatalf("relative link path=%s, scroll=%v, message=%q", state.session.Current().Path, state.previewScroll, state.message)
	}
	if target, ok := driver.LastOpenedURL(); ok {
		t.Fatalf("local link launched an external URL: %q", target)
	}
}

func m3PrimaryClick(t *testing.T, driver *app.Driver, x, y float32) {
	t.Helper()
	modifier := gfx.KeyControlLeft
	if primaryModifier() == gfx.ModifierMeta {
		modifier = gfx.KeyMetaLeft
	}
	if err := driver.KeyDown(modifier, ""); err != nil {
		t.Fatal(err)
	}
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if err := driver.KeyUp(modifier, ""); err != nil {
		t.Fatal(err)
	}
}

func TestM3EditingLinksNeedPrimaryClick(t *testing.T) {
	for _, rendered := range []bool{false, true} {
		name := "source"
		if rendered {
			name = "rendered"
		}
		t.Run(name, func(t *testing.T) {
			state, driver := testDriver(t)
			current := state.session.Current()
			prefix := "[go to section](#section)\n\n" + strings.Repeat("Padding text.\n\n", 50)
			source := prefix + "# Section\n\n" + strings.Repeat("Later text.\n\n", 40)
			current.Replace(source)
			current.Editor.MoveTo(0, false)
			if rendered {
				clickID(t, driver, renderedModeID)
			} else {
				state.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					t.Fatal(err)
				}
			}
			x, y := state.sourceRect.X+24, state.sourceRect.Y+12
			if rendered {
				if len(state.previewLinkHits) == 0 {
					t.Fatal("local link has no hit")
				}
				hit := state.previewLinkHits[0].bounds
				x, y = hit.X+hit.Width/2, hit.Y+hit.Height/2
			}
			if err := driver.Click(x, y); err != nil {
				t.Fatal(err)
			}
			if state.previewScroll > 0 || state.source.ScrollY() > 0 || current.Editor.Caret() >= len(prefix) {
				t.Fatal("ordinary edit click navigated")
			}
			m3PrimaryClick(t, driver, x, y)
			if err := driver.Advance(250 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if rendered && (!state.rich.active || state.rich.input.String() != "Section" || state.previewScroll < 400) {
				t.Fatalf("rendered anchor did not move: %v, %q", state.previewScroll, state.message)
			}
			if !rendered && (current.Editor.Caret() != len(prefix) || state.source.ScrollY() < 400) {
				t.Fatalf("source anchor did not move: %v at %v, %q", state.source.ScrollY(), current.Editor.Caret(), state.message)
			}
			if current.Text() != source {
				t.Fatal("navigation changed Markdown")
			}
		})
	}
}

func TestM3RenderedFocusAndTypewriterInBothThemes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "Github"
		if dark {
			name = "Night"
		}
		t.Run(name, func(t *testing.T) {
			state, driver := testDriver(t)
			if dark {
				clickID(t, driver, themeToggleID)
			}
			current := state.session.Current()
			current.Replace(strings.Repeat("Context.\n\n", 24) + "中文 active\n\n" + strings.Repeat("Later.\n\n", 24))
			clickID(t, driver, renderedModeID)
			delta := state.preview.blocks[24].top - 120
			if err := driver.Scroll(gfx.ScrollEvent{X: state.previewRect.X + 50, Y: state.previewRect.Y + 80, DeltaY: delta, Precise: true}); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedRows.At(24))
			if err := driver.Chord(primaryModifier()|gfx.ModifierAlt, "t"); err != nil {
				t.Fatal(err)
			}
			if err := driver.Chord(primaryModifier()|gfx.ModifierAlt, "d"); err != nil {
				t.Fatal(err)
			}
			if err := driver.Advance(200 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			caret, ok := state.rich.CaretBounds()
			if !ok || deltaAbs(caret.Y+caret.Height/2-(state.previewRect.Y+state.previewRect.Height/2)) > 24 {
				t.Fatalf("rendered caret not centered: %v in %v", caret, state.previewRect)
			}
			_, palette := state.look()
			matte := state.markdownAppearance(palette).palette.background
			matte.Alpha = 0.42
			var dimmedBlocks int
			for _, primitive := range driver.Snapshot().Primitives {
				if primitive.Color != matte {
					continue
				}
				dimmedBlocks++
				if contains(primitive.Bounds, ui.Point{X: caret.X, Y: caret.Y + caret.Height/2}) {
					t.Fatal("active block was dimmed")
				}
			}
			if dimmedBlocks < 2 {
				t.Fatal("context blocks were not dimmed")
			}
			state.rich.input.SetSelection(text.Range{Start: 0, End: 2})
			state.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				t.Fatal(err)
			}
			if !hasText(driver, "选中 2 /") {
				t.Fatal("rendered selection count missing")
			}
		})
	}
}

func TestM3MarkdownRelativePathRejectsEscapeForms(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "from.md")
	for _, destination := range []string{"../../outside.md", "/absolute.md", `..\..\outside.md`, "C:/outside.md", "../.. /outside.md", "else.md:stream", "nested/a\x00.md"} {
		if _, err := relativeMarkdownPath(root, path, destination); err == nil {
			t.Errorf("accepted unsafe %q", destination)
		}
	}
	for _, destination := range []string{"file:///unsafe.md", "https://example.org/doc.md", "//host/absolute.md", "javascript:alert(1)", "./x.md?anything=1"} {
		if _, ok := markdownLinkURL(destination); ok {
			t.Errorf("parsed non-local %q as local", destination)
		}
	}
	if resolved, err := relativeMarkdownPath(root, path, "../read me.md"); err != nil || resolved != "read me.md" {
		t.Fatalf("valid parent link = %q, %v", resolved, err)
	}
}

func TestM3PreferencesLoadLegacyAndSaveBothModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "settings.json")
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"dark":true,"autoSave":true,"sourceSize":18,"readingSize":22,"readingTheme":"soft"}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	store := session.PreferencesStore{Path: path}
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	if state.preferences.TypewriterMode || state.preferences.FocusMode {
		t.Fatal("legacy preferences unexpectedly enabled writing modes")
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, typewriterToggleID)
	clickID(t, driver, focusModeToggleID)
	loaded, err := store.Load()
	if err != nil || !loaded.TypewriterMode || !loaded.FocusMode || !loaded.Dark || !loaded.AutoSave || loaded.SourceSize != 18 || loaded.ReadingSize != 22 || loaded.ReadingTheme != "soft" {
		t.Fatalf("settings reload = %+v (%v)", loaded, err)
	}
}

func TestM3OutlineFollowsCaretAndScroll(t *testing.T) {
	state, driver := testDriver(t)
	current := state.session.Current()
	prefix := "# Parent\n\n## Child\n\n" + strings.Repeat("A paragraph.\n\n", 80)
	current.Replace(prefix + "# Other\n\n" + strings.Repeat("Last paragraph.\n\n", 80))
	current.Editor.MoveTo(10, false)
	clickID(t, driver, outlineToggleID)
	selected := func(index int) bool {
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == outlineRows.At(index) {
				return hit.Selected
			}
		}
		return false
	}
	if !selected(1) {
		t.Fatal("caret did not select Child in the outline")
	}
	clickID(t, driver, readModeID)
	if err := driver.Scroll(gfx.ScrollEvent{X: state.previewRect.X + 40, Y: state.previewRect.Y + 80, DeltaY: 100000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if !selected(2) {
		t.Fatal("scroll did not select Other in the outline")
	}
}
