package richedit

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseBuildsSegmentedVisibleTextWithoutRepeatedCopies(t *testing.T) {
	const spans = 1024
	text := strings.Repeat("segment &amp; \u4e2d ", 6)
	source := []byte("\xef\xbb\xbf<p>" + strings.Repeat("<strong>"+text+"</strong>", spans) + "</p>\r\n")
	expected := strings.Repeat(strings.ReplaceAll(text, "&amp;", "&"), spans)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	document := Parse(source)
	runtime.ReadMemStats(&after)
	if len(document.Blocks) != 1 || document.Blocks[0].Visible != expected || len(document.Blocks[0].Leaves) != spans {
		t.Fatal("segmented paragraph changed text or leaf count")
	}
	for _, leaf := range document.Blocks[0].Leaves {
		if !leaf.Bold || !leaf.Editable || leaf.Start < 0 || leaf.End < leaf.Start || leaf.End > len(source) || string(source[leaf.Start:leaf.End]) != text {
			t.Fatal("segmented paragraph changed a leaf's style or source mapping")
		}
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	if limit := uint64(len(source)) * 16; allocated > limit {
		t.Fatalf("segmented paragraph allocated %d bytes, limit %d", allocated, limit)
	}
	t.Logf("allocated=%d", allocated)
}

func TestParsePreservesVisibleAssemblyWithMalformedSuffix(t *testing.T) {
	const unknown = "<unknown>literal &amp;</unknown>"
	const malformed = `<a href="unterminated`
	source := "\xef\xbb\xbf<p><br></p>\r\n" + unknown + "\r\n<p><strong>A &amp; </strong><em>B</em><br><img src=pic.png alt=C>" + malformed
	document := Parse([]byte(source))
	if len(document.Blocks) != 3 {
		t.Fatalf("got %d blocks, want 3", len(document.Blocks))
	}
	empty, raw, mixed := document.Blocks[0], document.Blocks[1], document.Blocks[2]
	if empty.Visible != "" || empty.ReadOnly || len(empty.Leaves) != 1 || !empty.Leaves[0].Editable {
		t.Fatalf("empty break changed: %+v", empty)
	}
	if raw.Kind != Unknown || raw.Visible != unknown || !raw.ReadOnly || len(raw.Leaves) != 0 {
		t.Fatalf("raw fallback changed: %+v", raw)
	}
	if mixed.Visible != "A & B\nC"+malformed || !mixed.ReadOnly || mixed.ReadOnlyReason != "malformed HTML source" || mixed.End != len(source) || len(mixed.Leaves) != 4 {
		t.Fatalf("malformed block changed: %+v", mixed)
	}
	if !mixed.Leaves[0].Bold || !mixed.Leaves[1].Italic || mixed.Leaves[3].ImageSource != "pic.png" || !mixed.Leaves[3].SafeAsset {
		t.Fatalf("mixed inline metadata changed: %+v", mixed.Leaves)
	}
	for index, want := range []string{"A &amp; ", "B", "<br>", "<img src=pic.png alt=C>"} {
		leaf := mixed.Leaves[index]
		if leaf.Editable || leaf.Start < 0 || leaf.End < leaf.Start || leaf.End > len(source) || source[leaf.Start:leaf.End] != want {
			t.Fatalf("malformed block leaf %d changed: %+v", index, leaf)
		}
	}
}

func TestParseContextCancelsDuringVisibleAssembly(t *testing.T) {
	const spans = 3
	const checksBeforeAssembly = 1 + 1 + 3*spans + 1 + 1 + 1
	source := []byte("<p>" + strings.Repeat("<strong>next</strong>", spans) + "</p>")
	for _, phase := range []struct {
		name  string
		check int
	}{{"size", checksBeforeAssembly + 2}, {"copy", checksBeforeAssembly + spans + 2}} {
		t.Run(phase.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &tokenCancelContext{Context: parent, cancel: cancel, cancelAt: phase.check}
			document, err := ParseContext(ctx, source)
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(document, Document{}) {
				t.Fatalf("assembly returned %d blocks after %d checks, err=%v", len(document.Blocks), ctx.checks, err)
			}
		})
	}
}
