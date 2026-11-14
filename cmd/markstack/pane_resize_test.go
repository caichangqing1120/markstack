package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func paneHandle(t *testing.T, driver *app.Driver, label string) ui.Rect {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == label {
			return hit.Bounds
		}
	}
	t.Fatalf("resize handle %q is not visible", label)
	return ui.Rect{}
}

func dragPane(t *testing.T, driver *app.Driver, handle ui.Rect, x float32) {
	t.Helper()
	y := handle.Y + handle.Height/2
	if err := driver.Press(handle.X+handle.Width/2, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(x, y); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
}

func near(actual, expected float32) bool {
	return math.Abs(float64(actual-expected)) < 1
}

func paneBoundary(t *testing.T, driver *app.Driver, label string) float32 {
	t.Helper()
	handle := paneHandle(t, driver, label)
	return handle.X + handle.Width/2
}

func TestPaneResizeSidebarDragClampsAndRestoresWidth(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("unchanged")
	handle := paneHandle(t, driver, "调整文件侧栏宽度")
	dragPane(t, driver, handle, 360)
	if !near(state.treeRect.Width, 346) || !near(paneBoundary(t, driver, "调整文件侧栏宽度"), 360) {
		t.Fatalf("sidebar drag: tree=%+v document=%+v", state.treeRect, state.sourceRect)
	}
	dragPane(t, driver, paneHandle(t, driver, "调整文件侧栏宽度"), 1500)
	if !near(paneBoundary(t, driver, "调整文件侧栏宽度"), 420) || state.sourceRect.Width < 400 {
		t.Fatalf("wide drag did not clamp: %+v", state.sourceRect)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if !near(paneBoundary(t, driver, "调整文件侧栏宽度"), 240) || state.sourceRect.Width < 400 {
		t.Fatalf("narrow clamp: %+v", state.sourceRect)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if !near(paneBoundary(t, driver, "调整文件侧栏宽度"), 420) {
		t.Fatalf("preferred width was lost: %+v", state.sourceRect)
	}
	clickID(t, driver, sidebarToggleID)
	clickID(t, driver, sidebarToggleID)
	if !near(paneBoundary(t, driver, "调整文件侧栏宽度"), 420) || state.session.Current().Text() != "unchanged" {
		t.Fatalf("toggle or resize altered state: %+v %q", state.sourceRect, state.session.Current().Text())
	}
	dragPane(t, driver, paneHandle(t, driver, "调整文件侧栏宽度"), -50)
	if !near(paneBoundary(t, driver, "调整文件侧栏宽度"), 190) {
		t.Fatalf("left-edge clamp: %+v", state.sourceRect)
	}
}

func TestPaneResizeOutlineRetainsWidthWhenToggled(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Note\n\nText")
	clickID(t, driver, outlineToggleID)
	handle := paneHandle(t, driver, "调整大纲宽度")
	dragPane(t, driver, handle, 810)
	if !near(paneBoundary(t, driver, "调整大纲宽度"), 840) || state.sourceRect.Width < 400 {
		t.Fatalf("outline did not clamp: %+v", state.sourceRect)
	}
	clickID(t, driver, outlineToggleID)
	clickID(t, driver, outlineToggleID)
	if !near(paneBoundary(t, driver, "调整大纲宽度"), 840) {
		t.Fatalf("outline preferred width lost after toggle: %+v", state.sourceRect)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if state.sourceRect.Width < 400 || state.session.Current().Text() != "# Note\n\nText" {
		t.Fatalf("narrow outline obscured document: %+v", state.sourceRect)
	}
}

func TestPaneResizeConfirmationInterceptsDrag(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("dirty")
	handle := paneHandle(t, driver, "调整文件侧栏宽度")
	previous := paneBoundary(t, driver, "调整文件侧栏宽度")
	clickID(t, driver, tabClose.At(0))
	if state.session.Pending() == nil {
		t.Fatal("expected unsaved confirmation")
	}
	dragPane(t, driver, handle, 400)
	if !near(paneBoundary(t, driver, "调整文件侧栏宽度"), previous) || state.session.Current().Text() != "dirty" {
		t.Fatalf("modal leaked resize or altered draft: %+v", state.sourceRect)
	}
}

func TestPaneResizeKeyboardAdjustsOnlyFocusedPane(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Keep **these** bytes")
	initial := paneBoundary(t, driver, "调整文件侧栏宽度")
	handle := paneHandle(t, driver, "调整文件侧栏宽度")
	if err := driver.Click(handle.X+handle.Width/2, handle.Y+handle.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyRight, 0); err != nil {
		t.Fatal(err)
	}
	if !near(paneBoundary(t, driver, "调整文件侧栏宽度"), initial+16) {
		t.Fatal("right arrow did not grow the sidebar")
	}
	if err := driver.Key(gfx.KeyLeft, 0); err != nil {
		t.Fatal(err)
	}
	if !near(paneBoundary(t, driver, "调整文件侧栏宽度"), initial) {
		t.Fatal("left arrow did not restore the sidebar")
	}
	clickID(t, driver, outlineToggleID)
	initial = paneBoundary(t, driver, "调整大纲宽度")
	handle = paneHandle(t, driver, "调整大纲宽度")
	if err := driver.Click(handle.X+handle.Width/2, handle.Y+handle.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyLeft, 0); err != nil {
		t.Fatal(err)
	}
	if !near(paneBoundary(t, driver, "调整大纲宽度"), initial-16) {
		t.Fatal("left arrow did not grow the outline")
	}
	if state.session.Current().Text() != "# Keep **these** bytes" {
		t.Fatalf("pane keyboard action changed source: %q", state.session.Current().Text())
	}
}

func TestPaneResizeLongWorkspaceTitleFitsItsBounds(t *testing.T) {
	root := filepath.Join(t.TempDir(), "markstack-acceptance.he9akw")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	_, driver := testDriver(t)
	if err := driver.Resize(1605, 955); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	var measure gfx.Canvas
	for _, label := range driver.Snapshot().Texts {
		if label.Bounds.X == 14 && label.Bounds.Y == 43 {
			width := measure.TextWidthWeight(label.Value, label.Style.Size, label.Style.Weight)
			if width > label.Bounds.Width {
				t.Fatalf("workspace title %q draws %vpx into %vpx", label.Value, width, label.Bounds.Width)
			}
			return
		}
	}
	t.Fatal("workspace title not rendered")
}

func TestPaneResizeSearchUsesFileSidebarWidth(t *testing.T) {
	state, driver := testDriver(t)
	dragPane(t, driver, paneHandle(t, driver, "调整文件侧栏宽度"), 340)
	clickID(t, driver, workspaceOpenID)
	if !near(state.workspaceSearch.rect.Width+22, 340) || !near(paneBoundary(t, driver, "调整文件侧栏宽度"), 340) {
		t.Fatalf("search sidebar lost width: %+v", state.workspaceSearch.rect)
	}
	clickID(t, driver, workspaceBackID)
	if !near(state.treeRect.Width, 326) {
		t.Fatalf("file tree did not restore width: %+v", state.treeRect)
	}
}
