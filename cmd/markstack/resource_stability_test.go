package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/workspace"
)

// Bounded resource-stability regression for the roadmap's D20. Each test runs a
// small, fixed number of real cycles, waits for every worker it can observe by
// signal (never by a fixed sleep), and then asserts goroutines and open file
// descriptors return to the post-warmup band. Memory is recorded, not bounded,
// because a stable high-water mark is not a leak.
const (
	stabilitySearchCycles   = 100
	stabilityDocumentCycles = 100
	stabilityOverlayCycles  = 20
	stabilityWriterCycles   = 50
	stabilityGoroutineBand  = 4
	stabilityHandleBand     = 4
)

func stabilityOpenDescriptors(t *testing.T) (int, bool) {
	t.Helper()
	for _, root := range []string{"/dev/fd", "/proc/self/fd"} {
		entries, err := os.ReadDir(root)
		if err == nil {
			return len(entries), true
		}
	}
	return 0, false
}

func stabilityAssertDescriptors(t *testing.T, baseline int, stage string) {
	t.Helper()
	if got, ok := stabilityOpenDescriptors(t); ok && got > baseline+stabilityHandleBand {
		t.Fatalf("%s open descriptors=%d, warmup baseline=%d", stage, got, baseline)
	}
}

// stabilityGoroutines waits for NumGoroutine to come back inside the band after
// the workers signalled completion; the deadline only fails a regression, it
// never masks one.
func stabilityGoroutines(t *testing.T, baseline int, stage string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := runtime.NumGoroutine()
		if got <= baseline+stabilityGoroutineBand {
			return got
		}
		if time.Now().After(deadline) {
			buffer := make([]byte, 1<<20)
			n := runtime.Stack(buffer, true)
			t.Fatalf("%s goroutines=%d, warmup baseline=%d\n%s", stage, got, baseline, buffer[:n])
		}
		time.Sleep(time.Millisecond)
	}
}

func stabilityMemory(t *testing.T, stage string) {
	t.Helper()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	t.Logf("%s heap=%d heap-objects=%d gc=%d", stage, stats.HeapAlloc, stats.HeapObjects, stats.NumGC)
}

// TestWorkspaceSearchCancelCyclesReleaseWorkers runs repeated query/cancel
// cycles through the real streaming search worker. Every cycle must let the
// worker observe cancellation and return; cancelled generations must never leave
// results behind; the process must end the run with no extra goroutines or file
// descriptors.
func TestWorkspaceSearchCancelCyclesReleaseWorkers(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 8; index++ {
		body := "# 标题\n\n正文包含检索词 needle 与中文。\n"
		path := filepath.Join(root, "note-"+string(rune('a'+index))+".md")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := newModel()
	t.Cleanup(state.cancelWorkspaceSearch)
	var active atomic.Int64
	firstHit := make(chan struct{}, stabilitySearchCycles+1)
	exited := make(chan struct{}, stabilitySearchCycles+1)
	state.workspaceSearch.worker = func(ctx context.Context, root string, options workspace.SearchOptions, emit func(workspace.SearchHit) bool) (workspace.SearchSummary, error) {
		active.Add(1)
		defer func() {
			active.Add(-1)
			exited <- struct{}{}
		}()
		return workspace.Search(ctx, root, options, func(hit workspace.SearchHit) bool {
			if !emit(hit) {
				return false
			}
			firstHit <- struct{}{}
			<-ctx.Done()
			return false
		})
	}
	// Warmup reaches a real hit before cancelling the in-flight traversal, so
	// runtime resources are initialized before the baseline.
	state.startWorkspaceSearch(root, workspace.SearchOptions{Query: "needle"})
	select {
	case <-firstHit:
	case <-time.After(5 * time.Second):
		t.Fatal("warmup search did not produce a real hit")
	}
	state.drainWorkspaceSearch()
	state.cancelWorkspaceSearch()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("warmup search worker did not exit")
	}
	for range 2 {
		state.drainWorkspaceSearch()
		runtime.Gosched()
	}
	baselineGoroutines := runtime.NumGoroutine()
	baselineDescriptors, descriptorsAvailable := stabilityOpenDescriptors(t)
	stabilityMemory(t, "search warmup")

	for index := 0; index < stabilitySearchCycles; index++ {
		query := "needle"
		if index%3 == 0 {
			query = "中文"
		}
		state.startWorkspaceSearch(root, workspace.SearchOptions{Query: query})
		if !state.workspaceSearch.searching {
			t.Fatalf("cycle %d did not start a search", index)
		}
		select {
		case <-firstHit:
		case <-time.After(5 * time.Second):
			t.Fatalf("cycle %d search did not produce a real hit", index)
		}
		state.drainWorkspaceSearch()
		state.cancelWorkspaceSearch()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Fatalf("cycle %d search worker did not exit after cancel", index)
		}
	}
	state.drainWorkspaceSearch()
	if state.workspaceSearch.searching || len(state.workspaceSearch.results) != 0 || state.workspaceSearch.error != "" {
		t.Fatalf("leftover search state: searching=%v results=%d error=%q",
			state.workspaceSearch.searching, len(state.workspaceSearch.results), state.workspaceSearch.error)
	}
	if got := stabilityGoroutines(t, baselineGoroutines, "after search cycles"); got > baselineGoroutines+stabilityGoroutineBand {
		t.Fatalf("goroutines=%d baseline=%d", got, baselineGoroutines)
	}
	if descriptorsAvailable {
		stabilityAssertDescriptors(t, baselineDescriptors, "after search cycles")
	}
	stabilityMemory(t, "after search cycles")
}

// TestDocumentOpenCloseCyclesKeepHandlesStable repeatedly opens a real file and
// closes the clean tab, which is the short-lived document lifecycle behind tabs,
// drag-open and search navigation. Open descriptors and goroutines must not grow
// with the cycle count.
func TestDocumentOpenCloseCyclesKeepHandlesStable(t *testing.T) {
	root := t.TempDir()
	paths := make([]string, 3)
	for index := range paths {
		paths[index] = filepath.Join(root, "doc-"+string(rune('a'+index))+".md")
		if err := os.WriteFile(paths[index], []byte("# 标题\n\n正文 body。\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := newModel()
	cycle := func(index int) {
		t.Helper()
		if !state.openFile(paths[index%len(paths)]) {
			t.Fatalf("cycle %d: open %q failed: %s", index, paths[index%len(paths)], state.message)
		}
		if !state.session.RequestClose(state.session.Active) {
			t.Fatalf("cycle %d: clean document did not close", index)
		}
		if len(state.session.Tabs) != 1 || state.session.Current().Path != "" {
			t.Fatalf("cycle %d: session tabs=%d current=%q", index, len(state.session.Tabs), state.session.Current().Path)
		}
	}
	cycle(0)
	baselineGoroutines := runtime.NumGoroutine()
	baselineDescriptors, descriptorsAvailable := stabilityOpenDescriptors(t)
	stabilityMemory(t, "document warmup")
	for index := 0; index < stabilityDocumentCycles; index++ {
		cycle(index)
	}
	if got := stabilityGoroutines(t, baselineGoroutines, "after document cycles"); got > baselineGoroutines+stabilityGoroutineBand {
		t.Fatalf("goroutines=%d baseline=%d", got, baselineGoroutines)
	}
	if descriptorsAvailable {
		stabilityAssertDescriptors(t, baselineDescriptors, "after document cycles")
	}
	stabilityMemory(t, "after document cycles")
}

// TestImageInspectorAndExportCancelCyclesKeepHandlesStable opens and closes the
// image inspector and starts then cancels an HTML export repeatedly through the
// headless driver. Both are transient UI resources: cancelling the system save
// panel must release the export entirely.
func TestImageInspectorAndExportCancelCyclesKeepHandlesStable(t *testing.T) {
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	document := state.session.NewDocument()
	document.Replace("# 标题\n\n正文 body，用于取消导出。\n")
	inspector := func() {
		t.Helper()
		bitmap := gfx.Bitmap{Width: 8, Height: 8, Stride: 32, Pix: make([]byte, 8*8*4)}
		state.openImageInspectorBitmap(document, markdownPreviewImage{bitmap: bitmap, alt: "test"})
		if !state.imageInspector.open {
			t.Fatal("image inspector did not open")
		}
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
		if len(canvas.Images()) != 1 || state.imageInspector.geometry.Image.Width <= 0 {
			t.Fatal("image inspector did not paint its bitmap")
		}
		if err := driver.Key(gfx.KeyEscape, 0); err != nil {
			t.Fatal(err)
		}
		driver.Snapshot()
		if state.imageInspector.open || len(canvas.Images()) != 0 {
			t.Fatal("image inspector did not close and clear its image frame")
		}
	}
	export := func() {
		t.Helper()
		before := state.message
		if err := driver.QueueSaveFile("", app.ErrCanceled); err != nil {
			t.Fatal(err)
		}
		state.exportActiveHTML()
		if state.message != before {
			t.Fatalf("cancelled export changed the status: %q", state.message)
		}
	}
	inspector()
	export()
	baselineGoroutines := runtime.NumGoroutine()
	baselineDescriptors, descriptorsAvailable := stabilityOpenDescriptors(t)
	stabilityMemory(t, "overlay warmup")
	for index := 0; index < stabilityOverlayCycles; index++ {
		inspector()
		export()
	}
	if state.imageInspector.open {
		t.Fatal("image inspector left open")
	}
	if got := stabilityGoroutines(t, baselineGoroutines, "after overlay cycles"); got > baselineGoroutines+stabilityGoroutineBand {
		t.Fatalf("goroutines=%d baseline=%d", got, baselineGoroutines)
	}
	if descriptorsAvailable {
		stabilityAssertDescriptors(t, baselineDescriptors, "after overlay cycles")
	}
	stabilityMemory(t, "after overlay cycles")
}

// TestSnapshotWriterCloseCyclesReleaseWorker creates, schedules, flushes and
// closes the recovery/journal snapshot writer repeatedly. Close must join the
// writer goroutine and stay idempotent; a pending snapshot must be written
// exactly once per flush rather than accumulating workers.
func TestSnapshotWriterCloseCyclesReleaseWorker(t *testing.T) {
	baselineGoroutines := runtime.NumGoroutine()
	baselineDescriptors, descriptorsAvailable := stabilityOpenDescriptors(t)
	stabilityMemory(t, "writer warmup")
	for index := 0; index < stabilityWriterCycles; index++ {
		var saves atomic.Int64
		writer := newSnapshotWriter[int](func(int) error {
			saves.Add(1)
			return nil
		}, nil)
		writer.Schedule(index)
		if err := writer.Flush(); err != nil {
			t.Fatalf("cycle %d flush: %v", index, err)
		}
		if got := saves.Load(); got != 1 {
			t.Fatalf("cycle %d saves=%d, want 1", index, got)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("cycle %d close: %v", index, err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("cycle %d second close: %v", index, err)
		}
		if err := writer.Flush(); err != nil {
			t.Fatalf("cycle %d flush after close: %v", index, err)
		}
		select {
		case <-writer.done:
		default:
			t.Fatalf("cycle %d writer goroutine still running after close", index)
		}
	}
	if got := stabilityGoroutines(t, baselineGoroutines, "after writer cycles"); got > baselineGoroutines+stabilityGoroutineBand {
		t.Fatalf("goroutines=%d baseline=%d", got, baselineGoroutines)
	}
	if descriptorsAvailable {
		stabilityAssertDescriptors(t, baselineDescriptors, "after writer cycles")
	}
	stabilityMemory(t, "after writer cycles")
}
