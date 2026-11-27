package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/markdownedit"
)

func TestM2AutoTriggerDriver(test *testing.T) {
	for _, entry := range []struct {
		marker string
		kind   markdownedit.Kind
	}{{"# ", markdownedit.Heading}, {"## ", markdownedit.Heading}, {"### ", markdownedit.Heading}, {"#### ", markdownedit.Heading}, {"##### ", markdownedit.Heading}, {"###### ", markdownedit.Heading}, {"- ", markdownedit.ListItem}, {"* ", markdownedit.ListItem}, {"1. ", markdownedit.ListItem}, {"- [ ] ", markdownedit.ListItem}, {"> ", markdownedit.Quote}} {
		test.Run(entry.marker, func(test *testing.T) {
			state, driver, path, _ := renderedSourceCollisionFixture(test, "", true, 1200)
			for _, character := range entry.marker {
				if err := driver.Type(string(character)); err != nil {
					test.Fatal(err)
				}
			}
			if state.mode != renderedMode || state.rich.block.Kind != entry.kind || state.rich.input.String() != "" || state.session.Current().Text() != entry.marker {
				test.Fatalf("trigger %q: mode=%v block=%+v source=%q", entry.marker, state.mode, state.rich.block, state.session.Current().Text())
			}
			m1Shortcut(test, driver, primaryModifier(), 'z')
			if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != entry.marker {
				test.Fatalf("undo did not show the literal marker: %q", state.session.Current().Text())
			}
			m1Shortcut(test, driver, primaryModifier(), 'y')
			if state.rich.block.Kind != entry.kind || state.rich.input.String() != "" {
				test.Fatalf("redo did not restore the block: %+v", state.rich.block)
			}
			if err := driver.Type("正文"); err != nil {
				test.Fatal(err)
			}
			expectRenderedCollisionSave(test, state, driver, path, []byte("\xef\xbb\xbf"+entry.marker+"正文"))
		})
	}
}

func TestM2TriggerAtStartOfExistingParagraph(test *testing.T) {
	state, driver, path, _ := renderedSourceCollisionFixture(test, "正文\n\n尾段\n", false, 1200)
	clickID(test, driver, renderedRows.At(0))
	if err := driver.SelectText(0, 0); err != nil {
		test.Fatal(err)
	}
	for _, value := range []string{"#", " "} {
		if err := driver.Type(value); err != nil {
			test.Fatal(err)
		}
	}
	if state.rich.block.Kind != markdownedit.Heading || state.rich.input.String() != "正文" || state.rich.input.Caret() != 0 {
		test.Fatalf("heading did not retain body: %+v", state.rich.block)
	}
	expectRenderedCollisionSave(test, state, driver, path, []byte("\xef\xbb\xbf# 正文\r\n\r\n尾段\r\n"))
	m1Shortcut(test, driver, primaryModifier(), 'z')
	if state.rich.input.String() != "# 正文" || state.rich.input.Caret() != 2 {
		test.Fatal("literal undo did not restore line-start caret")
	}
}

func TestM2InlineDriver(test *testing.T) {
	for _, entry := range []struct{ typed, visible string }{{"前 **文字**", "前 文字"}, {"`代码`", "代码"}, {"前 `代码`", "前 代码"}} {
		test.Run(entry.typed, func(test *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(test, "", false, 1200)
			for _, character := range entry.typed {
				if err := driver.Type(string(character)); err != nil {
					test.Fatal(err)
				}
			}
			if !state.rich.active || state.rich.input.String() != entry.visible || state.rich.input.Caret() != len([]rune(entry.visible)) || state.session.Current().Text() != entry.typed || !state.activeMixedSpan() {
				test.Fatalf("misprojected inline input: source=%q mode=%v block=%+v", state.session.Current().Text(), state.mode, state.rich.block)
			}
			m1Shortcut(test, driver, primaryModifier(), 'z')
			if state.rich.input.String() != strings.TrimSuffix(entry.typed, entry.typed[len(entry.typed)-1:]) {
				test.Fatal("inline undo skipped typing history")
			}
			m1Shortcut(test, driver, primaryModifier(), 'y')
			if state.rich.input.String() != entry.visible {
				test.Fatal("inline redo did not restore styled text")
			}
		})
	}
	for _, entry := range []struct {
		key    rune
		markup string
	}{{'b', "**文字**"}, {'i', "*文字*"}, {'`', "`文字`"}, {'k', "[文字](https://example.com/a)"}} {
		test.Run(string(entry.key), func(test *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(test, "前 文字 后\n", false, 1200)
			clickID(test, driver, renderedRows.At(0))
			if err := driver.SelectText(2, 4); err != nil {
				test.Fatal(err)
			}
			driver.SetClipboard("https://example.com/a")
			m1Shortcut(test, driver, primaryModifier(), entry.key)
			if got := state.session.Current().Text(); got != "前 "+entry.markup+" 后\r\n" || driver.Snapshot().Focused != richInputID || state.rich.input.Selection() != (text.Range{Start: 2, End: 4}) {
				test.Fatalf("shortcut source=%q focus=%v", got, driver.Snapshot().Focused)
			}
			m1Shortcut(test, driver, primaryModifier(), 'z')
			if state.session.Current().Text() != "前 文字 后\r\n" {
				test.Fatal("inline shortcut requires more than one undo")
			}
		})
	}
}

func TestM2SlashDriver(test *testing.T) {
	for _, entry := range []struct {
		query, prefix string
		focus         gfx.ID
	}{{"tabl", "| 列 1 | 列 2 | 列 3 |", tableInputID}, {"code", "```\n\n```", codeInputID}, {"quote", "> ", richInputID}, {"todo", "- [ ] ", richInputID}, {"rule", "---\n\n", richInputID}, {"image", "![图片](assets/", richInputID}} {
		test.Run(entry.query, func(test *testing.T) {
			state, driver, path, _ := renderedSourceCollisionFixture(test, "", false, 1200)
			if entry.query == "image" {
				imageFile := filepath.Join(filepath.Dir(path), "selected.png")
				if err := os.WriteFile(imageFile, imagePNG(test, 20, 10), 0600); err != nil {
					test.Fatal(err)
				}
				if err := driver.QueueOpenFiles([]string{imageFile}, nil); err != nil {
					test.Fatal(err)
				}
			}
			if err := driver.Type("/"); err != nil {
				test.Fatal(err)
			}
			if len(state.slashMatches()) != 6 {
				test.Fatal("slash menu failed to open")
			}
			if err := driver.Type(entry.query); err != nil {
				test.Fatal(err)
			}
			if len(state.slashMatches()) != 1 {
				test.Fatal("slash menu did not filter")
			}
			if entry.query == "tabl" {
				for _, key := range []struct {
					key       gfx.Key
					modifiers gfx.Modifiers
				}{{gfx.KeyRight, 0}, {gfx.KeyDown, gfx.ModifierShift}} {
					if key.key == gfx.KeyRight {
						clickID(test, driver, slashSize.At(3))
						continue
					}
					if err := driver.Key(key.key, key.modifiers); err != nil {
						test.Fatal(err)
					}
				}
			}
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				test.Fatal(err)
			}
			inserted := state.session.Current().Text()
			if !strings.HasPrefix(inserted, entry.prefix) || state.slash.documentID != 0 || driver.Snapshot().Focused != entry.focus {
				test.Fatalf("slash insertion source=%q menu=%+v focus=%v", inserted, state.slash, driver.Snapshot().Focused)
			}
			if entry.query == "image" && strings.Contains(inserted, `\`) {
				test.Fatalf("image reference is not portable: %q", inserted)
			}
			m1Shortcut(test, driver, primaryModifier(), 'z')
			if state.session.Current().Text() != "/"+entry.query || state.slash.documentID != 0 {
				test.Fatal("slash undo did not restore typed query in one step")
			}
			m1Shortcut(test, driver, primaryModifier(), 'y')
			if state.session.Current().Text() != inserted {
				test.Fatal("slash redo failed")
			}
		})
	}
	state, driver, _, _ := renderedSourceCollisionFixture(test, "Keep\n", false, 1200)
	clickID(test, driver, renderedRows.At(0))
	m1Shortcut(test, driver, primaryModifier(), '/')
	if err := driver.Key(gfx.KeyDown, 0); err != nil {
		test.Fatal(err)
	}
	if state.slash.selected != 1 {
		test.Fatal("down did not highlight the next command")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		test.Fatal(err)
	}
	if state.slash.documentID != 0 || state.session.Current().Text() != "Keep\r\n" {
		test.Fatal("shortcut cancellation changed source or kept menu open")
	}
}

func TestM2ImageInspectorDriver(test *testing.T) {
	source := "![图](assets/old.png)\n\ninline ![two](<assets/old.png> 'original title')\n\n```md\n![example](assets/old.png)\n```\n"
	state, driver, path, _ := renderedSourceCollisionFixture(test, source, false, 1200)
	directory := filepath.Join(filepath.Dir(path), "assets")
	if err := os.Mkdir(directory, 0700); err != nil {
		test.Fatal(err)
	}
	oldFile, newFile := filepath.Join(directory, "old.png"), filepath.Join(directory, "renamed.png")
	if err := os.WriteFile(oldFile, imagePNG(test, 320, 180), 0600); err != nil {
		test.Fatal(err)
	}
	state.markdownMediaBlocks = nil
	clickID(test, driver, readModeID)
	width := state.preview.blocks[0].imageWidth
	clickID(test, driver, previewImages.At(0))
	for index, percent := range []string{"75%", "50%"} {
		clickID(test, driver, imageWidths.At(index+1))
		if !strings.Contains(state.session.Current().Text(), `assets/old.png "markstack-width=`+percent+`"`) {
			test.Fatalf("width source=%q", state.session.Current().Text())
		}
		if got := state.preview.blocks[0].imageWidth; got != width*float32(3-index)/4 {
			test.Fatalf("width was not immediately projected: %v", got)
		}
	}
	clickID(test, driver, imageWidths.At(0))
	if state.preview.blocks[0].imageWidth != width {
		test.Fatal("100% did not restore width")
	}
	clickID(test, driver, imageRenameID)
	if err := driver.Type("../outside"); err != nil {
		test.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		test.Fatal(err)
	}
	if state.imageInspector.errorText == "" || !strings.Contains(state.session.Current().Text(), "assets/old.png") {
		test.Fatal("invalid path was accepted")
	}
	if err := driver.SelectText(0, state.imageInspector.rename.RuneCount()); err != nil {
		test.Fatal(err)
	}
	if err := driver.Type("renamed"); err != nil {
		test.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		test.Fatal(err)
	}
	if _, err := os.Stat(newFile); err != nil {
		test.Fatal(err)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		test.Fatalf("old image still exists: %v", err)
	}
	want := strings.ReplaceAll(strings.Replace(source, "![图](assets/old.png)", "![图](assets/renamed.png)", 1), "<assets/old.png>", "<assets/renamed.png>")
	clickID(test, driver, imageCloseID)
	expectRenderedCollisionSave(test, state, driver, path, []byte("\xef\xbb\xbf"+strings.ReplaceAll(want, "\n", "\r\n")))
	m1Shortcut(test, driver, primaryModifier(), 'z')
	if _, err := os.Stat(oldFile); err != nil || state.session.Current().Text() != strings.ReplaceAll(source, "\n", "\r\n") {
		test.Fatalf("rename undo source=%q err=%v", state.session.Current().Text(), err)
	}
	m1Shortcut(test, driver, primaryModifier(), 'y')
	if _, err := os.Stat(newFile); err != nil || state.session.Current().Text() != strings.ReplaceAll(want, "\n", "\r\n") {
		test.Fatalf("rename redo source=%q err=%v", state.session.Current().Text(), err)
	}
}

func TestM2EnterTriggerAndIME(test *testing.T) {
	for _, marker := range []string{"---", "```go"} {
		test.Run(marker, func(test *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(test, "Head\n\nTail\n", true, 1200)
			clickID(test, driver, renderedRows.At(0))
			for _, key := range []gfx.Key{gfx.KeyEnd, gfx.KeyEnter} {
				if err := driver.Key(key, 0); err != nil {
					test.Fatal(err)
				}
			}
			before := state.session.Current().Text()
			if err := driver.Compose(marker); err != nil {
				test.Fatal(err)
			}
			if state.session.Current().Text() != before || state.codeBlock.active {
				test.Fatal("IME preedit transformed the source")
			}
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				test.Fatal(err)
			}
			if !state.rich.active || state.rich.input.String() != marker || state.codeBlock.active {
				test.Fatal("IME confirmation should commit only literal text")
			}
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				test.Fatal(err)
			}
			if source := state.session.Current().Text(); !strings.Contains(source, marker+"\r\n\r\n") || !strings.HasSuffix(source, "Tail\r\n") {
				test.Fatalf("enter trigger damaged CRLF or following text: %q", source)
			}
			if marker == "```go" && (!state.codeBlock.active || driver.Snapshot().Focused != codeInputID) {
				test.Fatal("fence did not focus the code body")
			}
			m1Shortcut(test, driver, primaryModifier(), 'z')
			if !state.rich.active || state.rich.input.String() != marker {
				test.Fatal("undo did not restore the literal opening marker")
			}
			m1Shortcut(test, driver, primaryModifier(), 'y')
			if marker == "```go" && !state.codeBlock.active {
				test.Fatal("redo did not restore code focus")
			}
		})
	}
}
