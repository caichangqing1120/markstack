package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The D19 search baseline runs against one deterministic, bounded workspace:
// searchBenchmarkFiles Markdown notes of exactly searchBenchmarkFileSize bytes
// in nested folders, plus the dependency and hidden directories Search must skip.
const (
	searchBenchmarkFiles    = 40
	searchBenchmarkFileSize = 4 << 10
	searchBenchmarkNeedle   = "基准检索词"
)

// Pinned corpus digest: if the generator changes, this fails before two
// baselines are compared. The benchmark logs the same digest with the size.
const (
	searchBenchmarkManifest   = "21fe2aa6081d7544f32b818105fe0e4bd8accbb35a876b26029ecffbf043aaf5"
	searchBenchmarkTotalBytes = searchBenchmarkFiles * searchBenchmarkFileSize
)

// buildSearchBenchmarkCorpus writes the deterministic tree under root and
// returns the SHA-256 manifest over relative paths, sizes and file digests.
func buildSearchBenchmarkCorpus(tb testing.TB, root string) (manifest string, total int64) {
	tb.Helper()
	digests := make(map[string]string, searchBenchmarkFiles)
	for index := 0; index < searchBenchmarkFiles; index++ {
		folder := fmt.Sprintf("notes/%02d", index%8)
		name := fmt.Sprintf("%02d-note.md", index)
		relative := filepath.ToSlash(filepath.Join(folder, name))
		body := searchBenchmarkNote(index)
		if len(body) != searchBenchmarkFileSize {
			tb.Fatalf("note %s bytes=%d, want %d", relative, len(body), searchBenchmarkFileSize)
		}
		path := filepath.Join(root, folder, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			tb.Fatal(err)
		}
		digests[relative] = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
		total += int64(len(body))
	}
	// Directories and file kinds Search must ignore; they are part of the corpus
	// fingerprint because the walk still visits their directory entries.
	excluded := map[string]string{
		".git/config":               "[core]\n\trepositoryformatversion = 0\n",
		"node_modules/pkg/index.js": "export const needle = \"" + searchBenchmarkNeedle + "\";\n",
		"vendor/example.go":         "package vendor\n",
		"notes/00/.hidden.md":       "# " + searchBenchmarkNeedle + "\n",
		"notes/binary.md":           "\x00\x01\x02 not UTF-8 \xff\xfe",
	}
	for relative, body := range excluded {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			tb.Fatal(err)
		}
		digests[relative] = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	}
	names := make([]string, 0, len(digests))
	for name := range digests {
		names = append(names, name)
	}
	sort.Strings(names)
	var manifestBuilder strings.Builder
	for _, name := range names {
		fmt.Fprintf(&manifestBuilder, "%s\x00%s\n", name, digests[name])
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(manifestBuilder.String()))), total
}

// searchBenchmarkNote builds one exactly searchBenchmarkFileSize byte note. Every
// fifth note contains the query, so literal and regex runs emit a bounded, known
// number of hits while still scanning and matching every file and line.
func searchBenchmarkNote(index int) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# 笔记 %02d overview\n\n", index)
	if index%5 == 0 {
		fmt.Fprintf(&builder, "本段包含%s用于正文搜索，位置固定。\n\n", searchBenchmarkNeedle)
	}
	for line := 0; builder.Len() < searchBenchmarkFileSize-64; line++ {
		switch line % 4 {
		case 0:
			fmt.Fprintf(&builder, "行 %d：中文内容与 Latin words 混排，用于检索基准。\n", line)
		case 1:
			builder.WriteString("- 列表项 with `inline code` and **强调**\n")
		case 2:
			builder.WriteString("| 列 A | 列 B |\n| --- | --- |\n| 值 | value |\n")
		default:
			builder.WriteString("普通段落 line with punctuation. 以及中文标点；重复。\n")
		}
	}
	body := builder.String()
	if len(body) > searchBenchmarkFileSize {
		body = strings.ToValidUTF8(body[:searchBenchmarkFileSize], "")
		for len(body) < searchBenchmarkFileSize {
			body += "\n"
		}
	}
	if len(body) < searchBenchmarkFileSize {
		body += strings.Repeat("\n", searchBenchmarkFileSize-len(body))
	}
	return body
}

// TestWorkspaceSearchBenchmarkCorpusIsDeterministic pins the corpus so a recorded
// search baseline can be reproduced from the repository alone: same paths, same
// bytes, same manifest digest.
func TestWorkspaceSearchBenchmarkCorpusIsDeterministic(t *testing.T) {
	root := t.TempDir()
	manifest, total := buildSearchBenchmarkCorpus(t, root)
	if total != searchBenchmarkTotalBytes {
		t.Errorf("corpus bytes=%d, want %d", total, searchBenchmarkTotalBytes)
	}
	if manifest != searchBenchmarkManifest {
		t.Errorf("corpus manifest sha256=%s, want %s", manifest, searchBenchmarkManifest)
	}
	again := t.TempDir()
	repeated, repeatedTotal := buildSearchBenchmarkCorpus(t, again)
	if repeated != manifest || repeatedTotal != total {
		t.Fatalf("corpus is not deterministic: %s/%d vs %s/%d", repeated, repeatedTotal, manifest, total)
	}
	hits := 0
	summary, err := Search(context.Background(), root, SearchOptions{Query: searchBenchmarkNeedle}, func(SearchHit) bool {
		hits++
		return true
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if hits == 0 || summary.Emitted != hits || summary.Truncated {
		t.Fatalf("corpus hits=%d emitted=%d truncated=%v", hits, summary.Emitted, summary.Truncated)
	}
}

// BenchmarkWorkspaceSearch measures the real read-only streaming search over the
// deterministic corpus: a literal body query, a regex body query and a
// Markdown-only literal query. Each run walks the opened root, skips hidden,
// dependency and non-UTF-8 entries, computes each file's fingerprint and emits
// every match, which is what the sidebar worker does per query.
func BenchmarkWorkspaceSearch(b *testing.B) {
	root := b.TempDir()
	manifest, total := buildSearchBenchmarkCorpus(b, root)
	b.Logf("corpus root=%s files=%d md-bytes=%d manifest-sha256=%s", root, searchBenchmarkFiles, total, manifest)
	options := []struct {
		name string
		opts SearchOptions
	}{
		{name: "literal", opts: SearchOptions{Query: searchBenchmarkNeedle}},
		{name: "regex", opts: SearchOptions{Query: "基准检索词|笔记 0[0-9] overview", Regex: true, CaseSensitive: true}},
		{name: "markdown-only", opts: SearchOptions{Query: searchBenchmarkNeedle, MarkdownOnly: true}},
	}
	for _, option := range options {
		b.Logf("search case %-13s options=%+v", option.name, option.opts)
	}
	for _, option := range options {
		option := option
		b.Run(option.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(total)
			b.ResetTimer()
			for range b.N {
				summary, err := Search(context.Background(), root, option.opts, func(SearchHit) bool { return true })
				if err != nil {
					b.Fatalf("search %s: %v", option.name, err)
				}
				if summary.Emitted == 0 {
					b.Fatalf("search %s emitted no hits", option.name)
				}
			}
		})
	}
}
