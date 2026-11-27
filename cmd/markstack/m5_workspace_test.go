package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"markstack/internal/markdownmedia"
	"markstack/internal/workspace"
)

func m5File(t *testing.T, root, relative, body string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func m5Wait(t *testing.T, driver *app.Driver, check func() bool) {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	for !check() {
		select {
		case <-driver.Wakes():
			if err := driver.Pump(); err != nil {
				t.Fatal(err)
			}
		case <-deadline.C:
			t.Fatal("workspace UI did not reach the expected state")
		}
	}
}

func m5OpenWorkspace(t *testing.T, driver *app.Driver, root string) {
	t.Helper()
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
}

func m5OpenFile(t *testing.T, driver *app.Driver, path string) {
	t.Helper()
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
}

func m5Poll(t *testing.T, state *model, driver *app.Driver) {
	t.Helper()
	state.diskWatch.due = time.Now().Add(-time.Second)
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
}

func TestM5DriverBacklinksNavigationInBothThemes(t *testing.T) {
	for _, theme := range []string{"Github", "Night"} {
		t.Run(theme, func(t *testing.T) {
			root := t.TempDir()
			target := m5File(t, root, "topic.md", "# 目标标题\n")
			prefix := strings.Repeat("填充行\r\n", 62)
			m5File(t, root, "notes/from.md", "\ufeff"+prefix+"参考 [跳转](../topic.md#section)\r\n")
			state, driver := testDriver(t)
			if theme == "Night" {
				clickID(t, driver, themeToggleID)
			}
			m5OpenWorkspace(t, driver, root)
			m5OpenFile(t, driver, target)
			clickID(t, driver, outlineToggleID)
			if !hasText(driver, "目标标题") {
				t.Fatal("outline is not available")
			}
			clickID(t, driver, backlinksTabID)
			m5Wait(t, driver, func() bool { return m3HasHit(driver, backlinkRows.At(0)) })
			if !hasText(driver, "notes/from.md:63") || !hasText(driver, "参考 [跳转]") {
				t.Fatal("missing backlink location or source context")
			}
			clickID(t, driver, backlinkRows.At(0))
			current := state.session.Current()
			if current.Title != "from.md" || current.Editor.Caret() != utf8.RuneCountInString(prefix+"参考 ") || current.Dirty() {
				t.Fatalf("current=%s caret=%d dirty=%v", current.Title, current.Editor.Caret(), current.Dirty())
			}
			clickID(t, driver, outlineTabID)
			if state.backlinks.scan.ready != nil || state.backlinks.selected {
				t.Fatal("switching to outline did not stop the backlink scan")
			}
		})
	}
}

func TestM5DriverMediaUsesSelectionConfirmationAndFreshAudit(t *testing.T) {
	root := t.TempDir()
	used := m5File(t, root, "assets/used.png", "123")
	orphan := m5File(t, root, "notes/assets/orphan.jpg", "4567")
	m5File(t, root, "notes/source.md", "![used](../assets/used.png)")
	state, driver := testDriver(t)
	m5OpenWorkspace(t, driver, root)
	clickID(t, driver, themeToggleID)
	var trashed []string
	state.mediaManager.trash = func(_ markdownmedia.AuditResult, _ string, image markdownmedia.AuditedImage) (workspace.TrashResult, error) {
		trashed = append(trashed, image.Relative)
		return workspace.TrashResult{}, nil
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, mediaMenuID)
	m5Wait(t, driver, func() bool { return m3HasHit(driver, mediaRows.At(0)) })
	if !hasText(driver, "总占用：7 字节") || !hasText(driver, "1 篇") {
		t.Fatal("total or reference count not shown")
	}
	clickID(t, driver, mediaRows.At(0))
	if !hasText(driver, "notes/source.md") {
		t.Fatal("reference document details not shown")
	}
	clickID(t, driver, mediaBackID)
	clickID(t, driver, mediaOrphansID)
	if !workspace.TrashAvailable() {
		if !hasText(driver, "不支持系统回收站") {
			t.Fatal("missing unavailable-Trash status")
		}
		return
	}
	clickID(t, driver, mediaRows.At(0))
	clickID(t, driver, mediaTrashID)
	clickID(t, driver, mediaCancelID)
	if len(trashed) != 0 || !m3HasHit(driver, mediaTrashID) {
		t.Fatal("cancel moved assets or forgot the selection")
	}
	clickID(t, driver, mediaTrashID)
	late := m5File(t, root, "notes/late.md", "![new reference](assets/orphan.jpg)")
	clickID(t, driver, mediaConfirmID)
	m5Wait(t, driver, func() bool { return hasText(driver, "未移动文件") })
	if len(trashed) != 0 {
		t.Fatal("a newly referenced image reached Trash")
	}
	if err := os.Remove(late); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, mediaRefreshID)
	m5Wait(t, driver, func() bool { return m3HasHit(driver, mediaRows.At(0)) })
	clickID(t, driver, mediaRows.At(0))
	clickID(t, driver, mediaTrashID)
	if len(trashed) != 0 {
		t.Fatal("first click reached Trash")
	}
	clickID(t, driver, mediaConfirmID)
	m5Wait(t, driver, func() bool { return len(trashed) == 1 })
	if trashed[0] != "notes/assets/orphan.jpg" {
		t.Fatalf("wrong Trash selection: %v", trashed)
	}
	for _, path := range []string{used, orphan} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("fixture unexpectedly touched: %v", err)
		}
	}
	clickID(t, driver, mediaCloseID)
	if state.mediaManager.open || state.mediaManager.scan.ready != nil {
		t.Fatal("media scan remains open")
	}
}

func TestM5DriverDiskWatcherProtectsDraftsAndStops(t *testing.T) {
	root := t.TempDir()
	path := m5File(t, root, "watch.md", "\ufefffirst\r\n")
	state, driver := testDriver(t)
	m5OpenFile(t, driver, path)
	doc := state.session.Current()
	m5File(t, root, "watch.md", "\ufeffsecond version\r\n")
	m5Wait(t, driver, func() bool { return m3HasHit(driver, diskReloadID) })
	if m3HasHit(driver, diskSaveAsID) || doc.Text() != "first\r\n" {
		t.Fatal("clean change unexpectedly applied or showed draft actions")
	}
	clickID(t, driver, diskIgnoreID)
	m5Poll(t, state, driver)
	if m3HasHit(driver, diskReloadID) {
		t.Fatal("same ignored revision reappeared")
	}
	m5File(t, root, "watch.md", "\ufeffthird external version\r\n")
	m5Poll(t, state, driver)
	clickID(t, driver, diskReloadID)
	if doc.Text() != "third external version\r\n" || doc.Dirty() || m3HasHit(driver, diskReloadID) {
		t.Fatalf("clean reload: %q dirty=%v", doc.Text(), doc.Dirty())
	}
	doc.Replace("my draft\r\n")
	m5File(t, root, "watch.md", "\ufefffourth external version\r\n")
	m5Poll(t, state, driver)
	clickID(t, driver, diskIgnoreID)
	if !doc.Dirty() || doc.Text() != "my draft\r\n" {
		t.Fatal("keeping the draft lost edits")
	}
	if _, err := driver.Shortcut('s'); err != nil {
		t.Fatal(err)
	}
	if disk, err := os.ReadFile(path); err != nil || string(disk) != "\ufefffourth external version\r\n" {
		t.Fatalf("original was overwritten: %q %v", disk, err)
	}
	clickID(t, driver, diskReloadID)
	clickID(t, driver, diskReloadCancelID)
	clickID(t, driver, diskReloadID)
	m5File(t, root, "watch.md", "\ufefffifth external version longer\r\n")
	clickID(t, driver, diskReloadConfirmID)
	if doc.Text() != "my draft\r\n" || !doc.Dirty() {
		t.Fatal("a stale confirmation discarded the draft")
	}
	copyPath := filepath.Join(root, "copy.md")
	if err := driver.QueueSaveFile(copyPath, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, diskSaveAsID)
	if copy, err := os.ReadFile(copyPath); err != nil || string(copy) != "\ufeffmy draft\r\n" || doc.Dirty() {
		t.Fatalf("Save As: %q %v dirty=%v", copy, err, doc.Dirty())
	}
	doc.Replace("discard me\r\n")
	m5File(t, root, "copy.md", "new disk copy\r\n")
	m5Poll(t, state, driver)
	clickID(t, driver, diskReloadID)
	clickID(t, driver, diskReloadConfirmID)
	if doc.Text() != "new disk copy\r\n" || doc.Dirty() {
		t.Fatalf("confirmed reload: %q dirty=%v", doc.Text(), doc.Dirty())
	}
	clickID(t, driver, tabClose.At(state.session.Active))
	if state.diskWatch.timer != nil || len(state.diskWatch.entries) != 0 {
		t.Fatal("watch remains armed with no saved tab")
	}
}

func TestM5DriverLargePreviewIsReadOnly(t *testing.T) {
	root := t.TempDir()
	path := m5File(t, root, "large.md", strings.Repeat("可浏览的前段\r\n", 5000))
	if err := os.Truncate(path, workspace.MaxDocumentSize+128); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	m5OpenFile(t, driver, path)
	current := state.session.Current()
	before := current.Text()
	if !current.ReadOnly() || len(before) > workspace.LargePreviewSize || !hasText(driver, largePreviewText) || !hasText(driver, "局部预览") {
		t.Fatalf("large view readonly=%v prefix=%d", current.ReadOnly(), len(before))
	}
	clickID(t, driver, sourceID)
	if err := driver.Type("must not appear"); err != nil && !strings.Contains(err.Error(), "no focused text field") {
		t.Fatal(err)
	}
	if err := driver.Scroll(gfx.ScrollEvent{X: state.sourceRect.X + 20, Y: state.sourceRect.Y + 20, DeltaY: 360}); err != nil || state.source.ScrollY() <= 0 {
		t.Fatalf("read-only preview did not scroll: %v", err)
	}
	if _, err := driver.Shortcut('2'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Shortcut('s'); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(state.session.Save(state.session.Active, filepath.Join(root, "copy.md")), workspace.ErrReadOnlyPreview) {
		t.Fatal("save was not rejected at the session boundary")
	}
	if info, err := os.Stat(path); err != nil || info.Size() != workspace.MaxDocumentSize+128 || before != current.Text() || current.Dirty() || state.mode != editMode {
		t.Fatalf("preview was changed: size/err=%v/%v dirty=%v mode=%v", info, err, current.Dirty(), state.mode)
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == sourceID && hit.ReadOnly && hit.Role == gfx.RoleTextField {
			return
		}
	}
	t.Fatal("source preview does not expose read-only semantics")
}
