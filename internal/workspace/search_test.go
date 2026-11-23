package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSearchStreamsUnicodeBOMAndCRLFOffsets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes", "sample.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("# 开头\r\n第二 目标\r\n")
	raw := append([]byte{0xef, 0xbb, 0xbf}, content...)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	var hits []SearchHit
	summary, err := Search(context.Background(), root, SearchOptions{Query: "目标"}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		return true
	})
	if err != nil || summary.Emitted != 1 || summary.Truncated || len(hits) != 1 {
		t.Fatalf("search summary=%+v hits=%+v err=%v", summary, hits, err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := SearchHit{
		Path: filepath.Join(canonicalRoot, "notes", "sample.md"), Relative: filepath.Join("notes", "sample.md"), Line: 2, Column: 4,
		Preview: "第二 目标", ByteOffset: len("# 开头\r\n第二 "), MatchBytes: len("目标"), Fingerprint: sha256.Sum256(raw),
	}
	if hits[0] != want {
		t.Fatalf("search hit=%+v, want=%+v", hits[0], want)
	}
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, raw) {
		t.Fatalf("search changed source bytes: %x, err=%v", stored, err)
	}
}

func TestSearchFindsEmptyLinesAtFileBoundaries(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const body = "\r\n\u524d\r\n\n"
	raw := []byte("\xef\xbb\xbf" + body)
	for name, content := range map[string][]byte{"empty.txt": nil, "lines.md": raw} {
		if err := os.WriteFile(filepath.Join(root, name), content, 0640); err != nil {
			t.Fatal(err)
		}
	}
	want := []SearchHit{
		{Path: filepath.Join(root, "empty.txt"), Relative: "empty.txt", Line: 1, Column: 1, Fingerprint: sha256.Sum256(nil)},
		{Path: filepath.Join(root, "lines.md"), Relative: "lines.md", Line: 1, Column: 1, Fingerprint: sha256.Sum256(raw)},
		{Path: filepath.Join(root, "lines.md"), Relative: "lines.md", Line: 3, Column: 1, ByteOffset: len(body) - 1, Fingerprint: sha256.Sum256(raw)},
		{Path: filepath.Join(root, "lines.md"), Relative: "lines.md", Line: 4, Column: 1, ByteOffset: len(body), Fingerprint: sha256.Sum256(raw)},
	}
	var hits []SearchHit
	summary, err := Search(context.Background(), root, SearchOptions{Query: "^$", Regex: true}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		return true
	})
	if err != nil || summary != (SearchSummary{Emitted: len(want)}) || !slices.Equal(hits, want) {
		t.Fatalf("search hits=%+v summary=%+v err=%v; want hits=%+v", hits, summary, err, want)
	}
}

func TestSearchMatchesNameBodyRegexCaseAndMarkdownScope(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"Mark-1.md": "Alpha foo42\n", "plain.txt": "alpha foo43\n", "draft.mdx": "alpha foo44\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	search := func(options SearchOptions) ([]SearchHit, SearchSummary, error) {
		var hits []SearchHit
		summary, err := Search(context.Background(), root, options, func(hit SearchHit) bool {
			hits = append(hits, hit)
			return true
		})
		return hits, summary, err
	}
	if hits, _, err := search(SearchOptions{Query: "mark", CaseSensitive: true}); err != nil || len(hits) != 0 {
		t.Fatalf("case-sensitive filename hits=%+v err=%v", hits, err)
	}
	if hits, _, err := search(SearchOptions{Query: "mark"}); err != nil || len(hits) != 1 || !hits[0].FileName || hits[0].Relative != "Mark-1.md" {
		t.Fatalf("filename hits=%+v err=%v", hits, err)
	}
	if hits, _, err := search(SearchOptions{Query: `foo4[23]`, Regex: true, MarkdownOnly: true}); err != nil || len(hits) != 1 || hits[0].Relative != "Mark-1.md" || hits[0].Line != 1 {
		t.Fatalf("regex Markdown hits=%+v err=%v", hits, err)
	}
	if hits, _, err := search(SearchOptions{Query: "ALPHA", MarkdownOnly: true}); err != nil || len(hits) != 2 {
		t.Fatalf("case-insensitive Markdown hits=%+v err=%v", hits, err)
	}
	if hits, _, err := search(SearchOptions{Query: "[", Regex: true}); err == nil || len(hits) != 0 {
		t.Fatalf("invalid regex hits=%+v err=%v", hits, err)
	}
}

func TestSearchKeepsIntentionalQueryWhitespace(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"joined.md": "tailX\n", "spaced.md": "tail end\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var hits []SearchHit
	_, err := Search(context.Background(), root, SearchOptions{Query: "tail "}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		return true
	})
	if err != nil || len(hits) != 1 || hits[0].Relative != "spaced.md" {
		t.Fatalf("space-sensitive search hits=%+v err=%v", hits, err)
	}
}

func TestSearchRichHTMLUsesVisibleBlocksNotAttributes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rich.mh")
	raw := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<p data-note="hidden-target"><strong>A &amp;</strong><em> 目标</em></p>`+"\r\n<h2>标题</h2>\r\n")...)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	search := func(query string, onlyMarkdown bool) []SearchHit {
		t.Helper()
		var hits []SearchHit
		if _, err := Search(context.Background(), root, SearchOptions{Query: query, MarkdownOnly: onlyMarkdown}, func(hit SearchHit) bool {
			hits = append(hits, hit)
			return true
		}); err != nil {
			t.Fatal(err)
		}
		return hits
	}
	hits := search("A & 目标", false)
	if len(hits) != 1 || !hits[0].Rich || hits[0].RichBlock != 0 || hits[0].Preview != "A & 目标" || hits[0].RichStart != 0 || hits[0].RichEnd != len("A & 目标") || hits[0].Fingerprint != sha256.Sum256(raw) {
		t.Fatalf("visible HTML hit=%+v", hits)
	}
	if hits := search("hidden-target", false); len(hits) != 0 {
		t.Fatalf("attribute appeared in body search: %+v", hits)
	}
	if hits := search("<h2>", false); len(hits) != 0 {
		t.Fatalf("HTML tag appeared in body search: %+v", hits)
	}
	if hits := search("目标", true); len(hits) != 0 {
		t.Fatalf("rich file appeared in Markdown-only scope: %+v", hits)
	}
}

func TestSearchCapsLongLinePreviewAroundMatch(t *testing.T) {
	root := t.TempDir()
	line := strings.Repeat("a", 220) + "目标" + strings.Repeat("b", 220)
	if err := os.WriteFile(filepath.Join(root, "long.md"), []byte(line+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var hits []SearchHit
	_, err := Search(context.Background(), root, SearchOptions{Query: "目标"}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		return true
	})
	if err != nil || len(hits) != 1 || !strings.Contains(hits[0].Preview, "目标") || !strings.HasPrefix(hits[0].Preview, "…") || !strings.HasSuffix(hits[0].Preview, "…") || len([]rune(hits[0].Preview)) > 162 {
		t.Fatalf("long preview hits=%+v err=%v", hits, err)
	}
}

func TestSearchSkipsHiddenDependenciesSymlinksAndBinary(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string][]byte{
		"visible.md":     []byte("needle\n"),
		".git/hidden.md": []byte("needle\n"), "node_modules/package.md": []byte("needle\n"),
		"vendor/dependency.md": []byte("needle\n"), "build/output.md": []byte("needle\n"),
		"binary.bin": []byte{'n', 'e', 'e', 'd', 'l', 'e', 0},
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("needle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	var hits []SearchHit
	_, err := Search(context.Background(), root, SearchOptions{Query: "needle"}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		return true
	})
	if err != nil || len(hits) != 1 || hits[0].Relative != "visible.md" {
		t.Fatalf("unsafe paths appeared in hits=%+v err=%v", hits, err)
	}
}

func TestSearchDoesNotFollowFileReplacedByOutsideSymlinkDuringScan(t *testing.T) {
	root, outsideRoot := t.TempDir(), t.TempDir()
	first, second := filepath.Join(root, "a.md"), filepath.Join(root, "b.md")
	outside := filepath.Join(outsideRoot, "outside.md")
	for path, body := range map[string]string{first: "needle first\n", second: "needle safe\n", outside: "needle outside\n"} {
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var hits []SearchHit
	_, err := Search(context.Background(), root, SearchOptions{Query: "needle"}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		if len(hits) == 1 {
			if err := os.Remove(second); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, second); err != nil {
				t.Fatal(err)
			}
		}
		return true
	})
	if err != nil || len(hits) != 1 || hits[0].Relative != "a.md" {
		t.Fatalf("outside file was searched after symlink swap: hits=%+v err=%v", hits, err)
	}
}

func TestSearchSkipsDirectoryReplacedByOutsideSymlinkDuringScan(t *testing.T) {
	root, outsideRoot := t.TempDir(), t.TempDir()
	inside := filepath.Join(root, "zdir")
	if err := os.Mkdir(inside, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "a.md"), filepath.Join(inside, "inside.md"), filepath.Join(outsideRoot, "outside.md")} {
		if err := os.WriteFile(path, []byte("needle\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var hits []SearchHit
	_, err := Search(context.Background(), root, SearchOptions{Query: "needle"}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		if len(hits) == 1 {
			if err := os.RemoveAll(inside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outsideRoot, inside); err != nil {
				t.Fatal(err)
			}
		}
		return true
	})
	if err != nil || len(hits) != 1 || hits[0].Relative != "a.md" {
		t.Fatalf("outside directory was searched after symlink swap: hits=%+v err=%v", hits, err)
	}
}

func TestSearchCancellationAndResultCap(t *testing.T) {
	root := t.TempDir()
	for index := range 5 {
		name := filepath.Join(root, "note-"+string(rune('a'+index))+".md")
		if err := os.WriteFile(name, []byte("match\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Search(ctx, root, SearchOptions{Query: "match"}, func(SearchHit) bool { t.Fatal("canceled search emitted a hit"); return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search error=%v", err)
	}
	count := 0
	summary, err := Search(context.Background(), root, SearchOptions{Query: "match", MaxResults: 2}, func(SearchHit) bool {
		count++
		return true
	})
	if err != nil || count != 2 || summary.Emitted != 2 || !summary.Truncated {
		t.Fatalf("capped search count=%d summary=%+v err=%v", count, summary, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	count = 0
	_, err = Search(ctx, root, SearchOptions{Query: "match"}, func(SearchHit) bool {
		count++
		cancel()
		return true
	})
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("midstream cancel count=%d err=%v", count, err)
	}
	if strings.Contains(filepath.Base(root), "needle") {
		t.Fatal("fixture path unexpectedly matched query")
	}
}

type richSearchCancelContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (ctx *richSearchCancelContext) Err() error {
	if ctx.remaining > 0 {
		ctx.remaining--
		if ctx.remaining == 0 {
			ctx.cancel()
		}
	}
	return ctx.Context.Err()
}

func TestSearchCancellationReachesActiveRichParse(t *testing.T) {
	root := t.TempDir()
	raw := []byte("\xef\xbb\xbf<p>needle" + strings.Repeat("<strong>words</strong>", 64) + "</p>\r\n")
	path := filepath.Join(root, "needle.mh")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &richSearchCancelContext{Context: parent, cancel: cancel}
	var hits []SearchHit
	summary, err := Search(ctx, root, SearchOptions{Query: "needle"}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		if hit.FileName {
			ctx.remaining = 8
		}
		return true
	})
	if !errors.Is(err, context.Canceled) || summary.Emitted != 1 || summary.Truncated || len(hits) != 1 || !hits[0].FileName {
		t.Fatalf("expected only the filename hit before cancellation: summary=%+v, hits=%+v, err=%v", summary, hits, err)
	}
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, raw) {
		t.Fatalf("search changed source bytes: %v", err)
	}
}

func TestSearchDefaultCapStopsAtFiveHundredHits(t *testing.T) {
	root := t.TempDir()
	var body strings.Builder
	for range 501 {
		body.WriteString("needle\n")
	}
	if err := os.WriteFile(filepath.Join(root, "many.md"), []byte(body.String()), 0644); err != nil {
		t.Fatal(err)
	}
	count, last := 0, 0
	summary, err := Search(context.Background(), root, SearchOptions{Query: "needle"}, func(hit SearchHit) bool {
		count++
		last = hit.Line
		return true
	})
	if err != nil || count != 500 || summary.Emitted != 500 || !summary.Truncated || last != 500 {
		t.Fatalf("default result cap count=%d last line=%d summary=%+v err=%v", count, last, summary, err)
	}
}

func TestSearchReportsDuplicateTextAtEachBOMCRLFPosition(t *testing.T) {
	root := t.TempDir()
	content := []byte("x alpha\r\n中间 alpha\r\n")
	raw := append([]byte{0xef, 0xbb, 0xbf}, content...)
	if err := os.WriteFile(filepath.Join(root, "dup.md"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	var hits []SearchHit
	summary, err := Search(context.Background(), root, SearchOptions{Query: "alpha"}, func(hit SearchHit) bool {
		hits = append(hits, hit)
		return true
	})
	if err != nil || summary.Emitted != 2 || len(hits) != 2 {
		t.Fatalf("duplicate search hits=%+v summary=%+v err=%v", hits, summary, err)
	}
	firstOffset, secondOffset := 2, len("x alpha\r\n")+len("中间 ")
	want := []struct {
		line, column, offset int
	}{{1, 3, firstOffset}, {2, 4, secondOffset}}
	for index, expected := range want {
		hit := hits[index]
		if hit.Line != expected.line || hit.Column != expected.column || hit.ByteOffset != expected.offset || hit.MatchBytes != len("alpha") || hit.Fingerprint != sha256.Sum256(raw) {
			t.Fatalf("duplicate hit %d=%+v, want line=%d column=%d offset=%d", index, hit, expected.line, expected.column, expected.offset)
		}
		if text := string(content[hit.ByteOffset : hit.ByteOffset+hit.MatchBytes]); text != "alpha" {
			t.Fatalf("hit %d offset slices %q", index, text)
		}
	}
	if hits[0].ByteOffset == hits[1].ByteOffset || hits[0].Preview == hits[1].Preview {
		t.Fatalf("duplicate positions collapsed: %+v", hits)
	}
}
