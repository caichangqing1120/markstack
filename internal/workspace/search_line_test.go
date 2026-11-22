package workspace

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

type longLineCancelContext struct {
	context.Context
	cancel context.CancelFunc
	armed  bool
	checks int
}

func TestBoundedSearchLineMatchesWholeLineAtWindowEdges(t *testing.T) {
	for _, sample := range []struct {
		pattern, tail string
	}{
		{`(?i)\p{Han}\p{So}[a-z]{2,4}`, "\u5c3e\U0001f642AbCdA"},
		{`\x{5c3e}(?:\x{1f642}a{1,6}|\x{1f642}a{1,2})`, "\u5c3e\U0001f642aaaaaa"},
		{`\x{5c3e}(?:\x{1f642}a{1,2}|\x{1f642}a{1,6})`, "\u5c3e\U0001f642aaaaaa"},
		{`\x{5c3e}\x{1f642}a{1,6}?`, "\u5c3e\U0001f642aaaaaa"},
		{`[\p{Han}\p{So}]{2}?(ab)?`, "\u5c3e\U0001f642ab"},
		{`missing|unseen`, "\u5c3e\U0001f642ab"},
		{`(?:missing)?`, "\u5c3e\U0001f642ab"},
	} {
		matcher := regexp.MustCompile(sample.pattern)
		overlap := boundedSearchRegexWidth(sample.pattern)
		if overlap < 0 {
			t.Fatalf("expected a bounded match: %q", sample.pattern)
		}
		for shift := -12; shift <= 12; shift++ {
			line := []byte(strings.Repeat("_", searchRegexWindow+shift) + sample.tail + strings.Repeat("_", searchRegexWindow))
			want := matcher.FindIndex(line)
			got, err := findBoundedSearchLine(context.Background(), matcher, line, overlap)
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("pattern %q shift %d: got %v (%v), want %v", sample.pattern, shift, got, err, want)
			}
		}
	}
}

func TestBoundedSearchRegexLeavesContextSensitivePatternsWhole(t *testing.T) {
	for _, pattern := range []string{`^tail`, `tail$`, `\Atail`, `tail\z`, `\btail`, `\Btail`, `ab*`, `ab+`, `a{2,}`, `a{1000}b{1000}`} {
		if got := boundedSearchRegexWidth(pattern); got >= 0 {
			t.Errorf("pattern %q must stay on the whole-line path, got width %d", pattern, got)
		}
	}
}

func (ctx *longLineCancelContext) Err() error {
	if ctx.armed {
		ctx.checks++
		if ctx.checks == 4 {
			ctx.cancel()
		}
	}
	return ctx.Context.Err()
}

func TestSearchCancelsInsideBoundedLongLine(t *testing.T) {
	checkBoundedLongLineCancellation(t, strings.Repeat("prefix marker ", 512<<10)+"needle-tail\r\n")
}

func TestSearchChecksCancellationBeforePublishingLongLine(t *testing.T) {
	checkBoundedLongLineCancellation(t, "needle-tail "+strings.Repeat("suffix marker ", 32<<10))
}

func checkBoundedLongLineCancellation(t *testing.T, line string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "rocket-tail.txt"), []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &longLineCancelContext{Context: parent, cancel: cancel}
	var hits []SearchHit
	summary, err := Search(ctx, root, SearchOptions{Query: `rocket-tail|needle-tail`, Regex: true, CaseSensitive: true}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		if hit.FileName {
			ctx.armed = true
		}
		return true
	})
	if !errors.Is(err, context.Canceled) || summary.Emitted != 1 || summary.Truncated || len(hits) != 1 || !hits[0].FileName {
		t.Fatalf("search continued past cancellation: checks=%d hits=%d summary=%+v err=%v", ctx.checks, len(hits), summary, err)
	}
}

func TestSearchBoundedLongLineMatchesFullHit(t *testing.T) {
	const prefix = "before\r\n"
	const match = "\u5c3e\U0001f642AbCd"
	before := strings.Repeat("_", (searchRegexWindow-4)/2) + strings.Repeat("\u00e9", (searchRegexWindow-4)/4) + "_"
	content := []byte("\xef\xbb\xbf" + prefix + before + match + "A " + match + "\r\n")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input.txt"), content, 0600); err != nil {
		t.Fatal(err)
	}
	search := func(ctx context.Context) ([]SearchHit, SearchSummary) {
		t.Helper()
		var hits []SearchHit
		summary, err := Search(ctx, root, SearchOptions{Query: `\p{Han}\p{So}[a-z]{2,4}`, Regex: true}, func(hit SearchHit) bool {
			hits = append(hits, hit)
			return true
		})
		if err != nil || summary.Emitted != 1 || summary.Truncated || len(hits) != 1 {
			t.Fatalf("long-line search returned %+v and %v", summary, err)
		}
		return hits, summary
	}
	want, wantSummary := search(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got, summary := search(ctx)
	if !slices.Equal(got, want) || summary != wantSummary {
		t.Fatalf("windowed search differed from whole-line search: got %+v, want %+v", got, want)
	}
	if hit := got[0]; hit.Line != 2 || hit.ByteOffset != len(prefix)+len(before) || hit.MatchBytes != len(match) || hit.Fingerprint != sha256.Sum256(content) {
		t.Fatalf("wrong long-line source identity: %+v", hit)
	}
}
