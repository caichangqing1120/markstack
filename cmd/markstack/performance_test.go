package main

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/markdown"

	"markstack/internal/markdownedit"
)

// The D19 performance baseline parses the same deterministic samples every run.
// Sizes are exact; the generator mixes the block kinds the rendered paint path
// meets (headings, inline styling, lists, quotes, tables, fenced code, CJK).
const (
	markdownParseSampleSmall = 10 << 10  // 10 KiB
	markdownParseSampleMid   = 100 << 10 // 100 KiB
	markdownParseSampleLarge = 1 << 20   // 1 MiB
)

// Pinned sample digests make the benchmark input reproducible: if the generator
// changes shape, this test fails before anyone compares two baselines.
const (
	markdownParseSmallDigest = "542fdc32455b9fc2b91341e3d8dc194eb9e7759271979f2828952c198944e48f"
	markdownParseMidDigest   = "8e0677766a2d7c5399f4b0af2fb7bd3c04ece2f2fed5676b45234fb7d7188783"
	markdownParseLargeDigest = "2997dc1633795bba19c65e6a08bf2b59ffbc3f787c814dd21789508e8e263943"
	markdownParseLongDigest  = "2b9b28764c080b292574a1d751e2faeed54370761a87818eb8e568eeb13880d9"
)

type markdownParseSample struct {
	name   string
	source []byte
}

// markdownParseSamples builds the D19 parse corpus: three sizes of mixed
// Markdown and one long single-line paragraph, each exactly its stated size.
func markdownParseSamples() []markdownParseSample {
	return []markdownParseSample{
		{name: "10KiB", source: mixedMarkdownSample(markdownParseSampleSmall)},
		{name: "100KiB", source: mixedMarkdownSample(markdownParseSampleMid)},
		{name: "1MiB", source: mixedMarkdownSample(markdownParseSampleLarge)},
		{name: "100KiB-long-line", source: longLineMarkdownSample(markdownParseSampleMid)},
	}
}

// mixedMarkdownSample writes mixed blocks until the buffer reaches size, cuts on
// a UTF-8 boundary and pads with newlines so the sample is exactly size bytes of
// valid UTF-8 Markdown. Every block variant appears in a fixed rotation, so the
// bytes are identical on every run and every machine.
func mixedMarkdownSample(size int) []byte {
	var builder strings.Builder
	builder.Grow(size + 512)
	for block := 0; builder.Len() < size; block++ {
		switch block % 7 {
		case 0:
			fmt.Fprintf(&builder, "## 章节 %d：中文标题与 English Heading\n\n", block)
		case 1:
			fmt.Fprintf(&builder, "正文段落 %d 包含中文、Latin words、**加粗强调**与*斜体*，以及 [链接](https://example.com/%d)。\n\n", block, block)
		case 2:
			fmt.Fprintf(&builder, "- 列表项 %d\n- second item with `inline code`\n\n", block)
		case 3:
			fmt.Fprintf(&builder, "> 引用段落 %d：引用内容包含中文与 Latin words。\n\n", block)
		case 4:
			fmt.Fprintf(&builder, "| 列 A | 列 B | 列 C |\n| --- | --- | --- |\n| %d | value %d | 中文 |\n\n", block, block)
		case 5:
			fmt.Fprintf(&builder, "```go\nfunc f%d() int { return %d }\n```\n\n", block, block)
		default:
			builder.WriteString("长段落中文内容 with Latin and 数字 1234567890 的重复内容。\n\n")
		}
	}
	return clampMarkdownSample(builder.String(), size, "\n")
}

// longLineMarkdownSample is one paragraph with no line break, the worst case for
// inline wrapping code that the renderer meets after parsing.
func longLineMarkdownSample(size int) []byte {
	unit := "长行中文 with Latin words 和 **强调** 与 *斜体*，用于压测单行排版。"
	var builder strings.Builder
	builder.Grow(size + 4*len(unit))
	for builder.Len() < size {
		builder.WriteString(unit)
	}
	return clampMarkdownSample(builder.String(), size, " ")
}

func clampMarkdownSample(value string, size int, pad string) []byte {
	value = strings.ToValidUTF8(value, "")
	if len(value) > size {
		cut := size
		for cut > 0 && !utf8.RuneStart(value[cut]) {
			cut--
		}
		value = value[:cut]
	}
	if len(value) < size {
		value += strings.Repeat(pad, size-len(value))
	}
	return []byte(value)
}

// TestMarkdownPerformanceSamplesAreDeterministic pins the benchmark corpus: the
// same bytes, size and SHA-256 on every run. The benchmark logs these values, so
// a recorded baseline can be reproduced from this test alone.
func TestMarkdownPerformanceSamplesAreDeterministic(t *testing.T) {
	want := map[string]struct {
		size   int
		digest string
	}{
		"10KiB":            {markdownParseSampleSmall, markdownParseSmallDigest},
		"100KiB":           {markdownParseSampleMid, markdownParseMidDigest},
		"1MiB":             {markdownParseSampleLarge, markdownParseLargeDigest},
		"100KiB-long-line": {markdownParseSampleMid, markdownParseLongDigest},
	}
	built := markdownParseSamples()
	if len(built) != len(want) {
		t.Fatalf("samples=%d, want %d", len(built), len(want))
	}
	for _, sample := range built {
		expected, ok := want[sample.name]
		if !ok {
			t.Fatalf("unexpected sample %q", sample.name)
		}
		if len(sample.source) != expected.size {
			t.Errorf("sample %s bytes=%d, want %d", sample.name, len(sample.source), expected.size)
		}
		if !utf8.Valid(sample.source) {
			t.Errorf("sample %s is not valid UTF-8", sample.name)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(sample.source))
		if digest != expected.digest {
			t.Errorf("sample %s sha256=%s, want %s", sample.name, digest, expected.digest)
		}
		var document markdown.Document
		document.Parse(string(sample.source))
		if len(document.Blocks) == 0 {
			t.Errorf("sample %s rendered blocks=0", sample.name)
		}
		if blocks := markdownedit.Parse(sample.source); len(blocks) == 0 {
			t.Errorf("sample %s source blocks=0", sample.name)
		}
	}
	again := markdownParseSamples()
	for index := range built {
		if string(built[index].source) != string(again[index].source) {
			t.Fatalf("sample %s is not deterministic", built[index].name)
		}
	}
}

// BenchmarkMarkdownParse measures the parse path the rendered view runs on every
// paint: the Viva Markdown document parse that produces the rendered blocks and
// the markdownedit source-range parse that maps them back to bytes
// (paintPreview calls both, and so does BenchmarkMarkdownParse's app-paint
// sub-benchmark). The render and blocks sub-benchmarks attribute the cost.
func BenchmarkMarkdownParse(b *testing.B) {
	b.Logf("environment: goos=%s goarch=%s go=%s", runtime.GOOS, runtime.GOARCH, runtime.Version())
	for _, sample := range markdownParseSamples() {
		sample := sample
		source := sample.source
		text := string(source)
		digest := sha256.Sum256(source)
		b.Run(sample.name, func(b *testing.B) {
			b.Logf("sample name=%s bytes=%d sha256=%x", sample.name, len(source), digest)
			b.Run("render", func(b *testing.B) {
				b.SetBytes(int64(len(source)))
				b.ReportAllocs()
				var document markdown.Document
				b.ResetTimer()
				for range b.N {
					document.Parse(text)
				}
			})
			b.Run("blocks", func(b *testing.B) {
				b.SetBytes(int64(len(source)))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					markdownedit.Parse(source)
				}
			})
			b.Run("app-paint", func(b *testing.B) {
				b.SetBytes(int64(len(source)))
				b.ReportAllocs()
				var document markdown.Document
				b.ResetTimer()
				for range b.N {
					document.Parse(text)
					markdownedit.Parse(source)
				}
			})
		})
	}
}
