package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	htmlnode "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func TestMarkdownHTMLExportIsSelfContainedAndKeepsDirtySource(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "p.png"), imagePNG(t, 2, 1), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.session.Current().Replace("# Export\n\n![Local](assets/p.png)\n")
	before := state.session.RecoverySnapshot()
	target := filepath.Join(t.TempDir(), "portable.html")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	output, err := os.ReadFile(target)
	if err != nil || !bytes.Contains(output, []byte("data:image/png;base64,")) || !bytes.Contains(output, []byte("<h1>Export</h1>")) {
		t.Fatalf("export HTML=%q err=%v message=%q", output, err, state.message)
	}
	if original, err := os.ReadFile(path); err != nil || string(original) != "# Original\n" {
		t.Fatalf("export overwrote Markdown: %q, %v", original, err)
	}
	current := state.session.Current()
	if current.Path != path || !current.Dirty() || current.Text() != before.Drafts[0].Body || state.session.RecoverySnapshot().Drafts[0].Body != before.Drafts[0].Body {
		t.Fatalf("export changed tab or recovery: path=%q dirty=%v", current.Path, current.Dirty())
	}
}

func TestHTMLExportCancelExistingTargetAndExternalConflictLeaveFilesIntact(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.session.Current().Replace("# Draft\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueSaveFile("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	if state.session.Current().Text() != "# Draft\n" || !state.session.Current().Dirty() {
		t.Fatal("canceled export changed draft")
	}
	target := filepath.Join(root, "existing.html")
	if err := os.WriteFile(target, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	if data, err := os.ReadFile(target); err != nil || string(data) != "keep" || state.message == "" {
		t.Fatalf("existing target=%q, %v, message=%q", data, err, state.message)
	}
	if err := os.WriteFile(path, []byte("# External\n"), 0644); err != nil {
		t.Fatal(err)
	}
	newTarget := filepath.Join(root, "fresh.html")
	if err := driver.QueueSaveFile(newTarget, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	if _, err := os.Stat(newTarget); !os.IsNotExist(err) {
		t.Fatalf("external conflict still wrote export: %v", err)
	}
	if state.session.Current().Text() != "# Draft\n" || !state.session.Current().Dirty() {
		t.Fatal("external conflict changed draft")
	}
}

func TestHTMLExportRejectsSymlinkSwappedSource(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.session.Current().Replace("# Draft\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	outsideSource := filepath.Join(root, "outside-source.md")
	if err := os.WriteFile(outsideSource, []byte("# Outside\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSource, path); err != nil {
		t.Fatal(err)
	}
	swapTarget := filepath.Join(root, "swapped.html")
	if err := driver.QueueSaveFile(swapTarget, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	if _, err := os.Stat(swapTarget); !os.IsNotExist(err) || state.message == "" {
		t.Fatalf("symlink-swapped source wrote export: %v message=%q", err, state.message)
	}
	if data, err := os.ReadFile(outsideSource); err != nil || string(data) != "# Outside\n" {
		t.Fatalf("symlink-swapped source target changed: %q, %v", data, err)
	}
	if state.session.Current().Text() != "# Draft\n" || !state.session.Current().Dirty() {
		t.Fatal("symlink-swapped source changed draft")
	}
}

func TestRichHTMLExportSanitizesUnknownNodesWithoutSavingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.mh")
	source := `<p data-id="keep">A &amp; <strong>B</strong></p><x-custom>Visible</x-custom><script>alert(1)</script>`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "rich.html")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	output, err := os.ReadFile(target)
	if err != nil || !bytes.Contains(output, []byte("<strong>B</strong>")) || !bytes.Contains(output, []byte("Visible")) || !bytes.Contains(output, []byte("&lt;script&gt;")) || bytes.Contains(output, []byte("<script")) {
		t.Fatalf("rich export=%q, %v", output, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != source || state.session.Current().Path != path || state.session.Current().Dirty() {
		t.Fatalf("rich source changed=%q, %v", data, err)
	}
}

func TestCodeAndPlainCannotExportHTMLThroughFileMenu(t *testing.T) {
	for _, extension := range []string{"txt", "go"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note."+extension)
			if err := os.WriteFile(path, []byte("<script>literal</script>"), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, fileMenuID)
			found := false
			for _, hit := range driver.Snapshot().Hits {
				if hit.ID == menuExportHTMLID {
					found = true
					if !hit.Disabled || !strings.Contains(hit.Label, "导出") {
						t.Fatalf("unavailable export is actionable: %+v", hit)
					}
				}
			}
			if !found {
				t.Fatal("export unavailable state is hidden")
			}
			clickID(t, driver, menuExportHTMLID)
			if state.session.Current().Dirty() {
				t.Fatal("disabled export changed document")
			}
		})
	}
}

func TestRichHTMLExportEmbedsOnlyLocalImageAndRejectsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.mh")
	source := `<p><img src="assets/p.png" alt="local"></p>`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "p.png"), imagePNG(t, 2, 1), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked.html")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueSaveFile(linked, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	if data, err := os.ReadFile(outside); err != nil || string(data) != "private" || state.message == "" {
		t.Fatalf("symlink target changed: %q, %v, message=%q", data, err, state.message)
	}
	target := filepath.Join(root, "rich.html")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	output, err := os.ReadFile(target)
	if err != nil || !bytes.Contains(output, []byte("data:image/png;base64,")) || bytes.Contains(output, []byte(`src="assets/p.png"`)) {
		t.Fatalf("rich image export=%q, %v", output, err)
	}
	if original, err := os.ReadFile(path); err != nil || string(original) != source {
		t.Fatalf("rich source changed: %q, %v", original, err)
	}
}

// TestHTMLExportRefusesUncommittedComposition covers the "no uncommitted
// composition may leave through export" contract for both a Markdown source
// editor IME composition and an active rich-text composition.
func TestHTMLExportRefusesUncommittedComposition(t *testing.T) {
	t.Run("markdown", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "note.md")
		original := []byte("# 原文\n")
		if err := os.WriteFile(path, original, 0644); err != nil {
			t.Fatal(err)
		}
		state, driver := testDriver(t)
		state.openFile(path)
		current := state.session.Current()
		current.Editor.SetMarkedText("pin", text.Range{Start: 3, End: 3})
		target := filepath.Join(t.TempDir(), "composing.html")
		if err := driver.QueueSaveFile(target, nil); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, fileMenuID)
		clickID(t, driver, menuExportHTMLID)
		if !strings.Contains(state.message, "输入法") || current.Dirty() {
			t.Fatalf("preedit export message=%q dirty=%v", state.message, current.Dirty())
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("preedit export wrote a target: %v", err)
		}
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) {
			t.Fatalf("preedit export changed source: %q, %v", data, err)
		}
	})
	t.Run("rich text", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "note.mh")
		source := "<p>原文</p>"
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		state, driver := testDriver(t)
		state.openFile(path)
		if err := driver.Resize(1200, 780); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, htmlLeafRows.At(0))
		if err := driver.Compose("pin"); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "composing-rich.html")
		if err := driver.QueueSaveFile(target, nil); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, fileMenuID)
		clickID(t, driver, menuExportHTMLID)
		if !strings.Contains(state.message, "输入法") || state.session.Current().Dirty() || state.session.Current().Text() != source {
			t.Fatalf("rich preedit export message=%q dirty=%v source=%q", state.message, state.session.Current().Dirty(), state.session.Current().Text())
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("rich preedit export wrote a target: %v", err)
		}
	})
}

// TestHTMLExportLongMixedDraftIsCompleteAndLeavesIdentityUntouched exports a
// committed in-memory draft with long Chinese mixed content, a wide table, an
// image inside a table cell, lists, unknown nodes and malicious attributes. The
// written file must contain every expected run with an inert DOM, and the tab
// path, dirty state, journal and on-disk bytes must stay exactly as they were.
func TestHTMLExportLongMixedDraftIsCompleteAndLeavesIdentityUntouched(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "cell.png"), imagePNG(t, 3, 2), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	var draft strings.Builder
	draft.WriteString("# 导出完整长文\n\n")
	for index := 1; index <= 20; index++ {
		fmt.Fprintf(&draft, "第%02d段：中文混排内容，含 **加粗**、*斜体*、`代码` 与 [链接](https://example.org/%d)。\n\n", index, index)
	}
	draft.WriteString("- 列表甲\n- 列表乙\n\n1. 有序甲\n2. 有序乙\n\n")
	draft.WriteString("| 第一列 | 第二列 | 第三列 | 第四列 | 第五列 | 第六列 |\n| --- | --- | --- | --- | --- | --- |\n")
	draft.WriteString("| ![单元格](assets/cell.png) | 右一 | 右二 | 右三 | 右四 | 右五 |\n")
	draft.WriteString("| 甲 | 乙 | 丙 | 丁 | 戊 | 己 |\n\n")
	draft.WriteString("<script>alert(1)</script>\n\n<x-widget onclick=\"alert(2)\">未知节点</x-widget>\n\n[危险](javascript:alert(3))\n")
	state.session.Current().Replace(draft.String())
	before := state.session.RecoverySnapshot()
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "complete.html")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportHTMLID)
	output, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	assertExportedHTML(t, output)
	if !bytes.Contains(output, []byte("data:image/png;base64,")) || !bytes.Contains(output, []byte("<table>")) {
		t.Fatalf("export omitted the table image or table: %d bytes", len(output))
	}
	document, err := htmlnode.Parse(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	var cells []*htmlnode.Node
	var visit func(*htmlnode.Node)
	visit = func(node *htmlnode.Node) {
		if node.Type == htmlnode.ElementNode && node.DataAtom == atom.Td {
			cells = append(cells, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	tabledImage := false
	for _, cell := range cells {
		var images []*htmlnode.Node
		var scan func(*htmlnode.Node)
		scan = func(node *htmlnode.Node) {
			if node.Type == htmlnode.ElementNode && node.DataAtom == atom.Img {
				images = append(images, node)
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				scan(child)
			}
		}
		scan(cell)
		tabledImage = tabledImage || len(images) > 0
	}
	if len(cells) != 12 || !tabledImage {
		t.Fatalf("wide table cells=%d image inside cell=%v", len(cells), tabledImage)
	}
	for _, expected := range []string{
		"第01段", "第20段", "加粗", "斜体", "代码", "链接", "列表甲", "列表乙", "有序甲", "有序乙",
		"第一列", "第六列", "右一", "右五", "甲", "己", "未知节点", "危险", "原始 HTML",
	} {
		if !bytes.Contains(output, []byte(expected)) {
			t.Errorf("export lost %q", expected)
		}
	}
	for _, forbidden := range []string{"<script", `onclick="`, `href="javascript:`} {
		if bytes.Contains(output, []byte(forbidden)) {
			t.Errorf("export kept active markup %q", forbidden)
		}
	}
	current := state.session.Current()
	if current.Path != path || !current.Dirty() || current.Text() != draft.String() {
		t.Fatalf("export changed tab identity: path=%q dirty=%v", current.Path, current.Dirty())
	}
	if after := state.session.RecoverySnapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("export changed the recovery journal snapshot")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "# Original\n" {
		t.Fatalf("export changed source bytes: %q, %v", data, err)
	}
	if afterInfo, err := os.Stat(path); err != nil || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) || afterInfo.Mode() != beforeInfo.Mode() {
		t.Fatalf("export touched the source file metadata: %v, %v", afterInfo, err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("export target metadata=%v, %v", info, err)
	}
}
