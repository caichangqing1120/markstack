package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

func TestCodeLanguageUsesOnlySupportedExtensions(t *testing.T) {
	for _, test := range []struct {
		path, language string
	}{
		{"main.go", "go"}, {"app.js", "javascript"}, {"view.tsx", "typescript"},
		{"data.json", "json"}, {"config.yaml", "yaml"}, {"page.html", "html"},
		{"icon.svg", "xml"}, {"style.css", "css"}, {"script.py", "python"},
		{"query.sql", "sql"}, {"build.sh", "bash"},
		{"note.md", ""}, {"rich.mh", ""}, {"plain.txt", ""}, {"unknown.xyz", ""},
	} {
		if got := codeLanguage(test.path); got != test.language {
			t.Errorf("code language of %q = %q, want %q", test.path, got, test.language)
		}
	}
}

func TestCodeHighlightKeepsMultilineContextForIdenticalLines(t *testing.T) {
	state := newModel()
	_, palette := state.look()
	for _, test := range []struct {
		name, language, source string
		inside, outside        int
	}{
		{"go block comment", "go", "package main\n/*\nreturn\n*/\nreturn\n", 2, 4},
		{"javascript template literal", "javascript", "const text = `\nreturn\n`;\nreturn;\n", 1, 3},
		{"python triple quoted string", "python", "text = \"\"\"\nreturn\n\"\"\"\nreturn\n", 1, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cache codeHighlightCache
			cache.Prepare(1, test.source, test.language, palette, false)
			lines := strings.Split(test.source, "\n")
			inside := cache.Runs(test.inside, lines[test.inside], nil)
			outside := cache.Runs(test.outside, lines[test.outside], nil)
			if len(inside) == 0 || len(outside) == 0 || inside[0].Color == outside[0].Color || cache.source != test.source {
				t.Fatalf("multiline context inside=%+v outside=%+v", inside, outside)
			}
		})
	}
	var cache codeHighlightCache
	keywordSource := "package main\n/*\nreturn\n*/\nreturn\n"
	cache.Prepare(1, keywordSource, "go", palette, false)
	keyword := cache.Runs(4, "return", nil)
	updated := "package main\nreturn\n"
	cache.Prepare(1, updated, "go", palette, false)
	if fresh := cache.Runs(1, "return", nil); len(fresh) == 0 || fresh[0].Color != keyword[0].Color {
		t.Fatalf("cache did not invalidate changed source: %+v", fresh)
	}
}

func TestCodeHighlightUsesUTF8ByteBoundariesAndBoundsLargeInput(t *testing.T) {
	state := newModel()
	_, palette := state.look()
	var cache codeHighlightCache
	source := "const 名称 = \"值\"\n"
	cache.Prepare(7, source, "javascript", palette, false)
	line := strings.TrimSuffix(source, "\n")
	for _, run := range cache.Runs(0, line, nil) {
		if run.Start < 0 || run.End > len(line) || run.End <= run.Start || !utf8.ValidString(line[:run.Start]) || !utf8.ValidString(line[:run.End]) {
			t.Fatalf("highlight split UTF-8 rune: %+v", run)
		}
	}
	large := strings.Repeat("x", maxCodeHighlightBytes+1)
	cache.Prepare(8, large, "go", palette, false)
	if runs := cache.Runs(0, large, nil); len(runs) != 0 || cache.source != large {
		t.Fatalf("large source was tokenized or changed: %d runs", len(runs))
	}
}

func TestCodeHighlightSupportsEveryDeclaredLexerAndCRLF(t *testing.T) {
	state := newModel()
	_, palette := state.look()
	var cache codeHighlightCache
	for index, path := range []string{"main.go", "app.js", "view.tsx", "data.json", "config.yaml", "page.html", "icon.svg", "style.css", "script.py", "query.sql", "build.sh"} {
		language := codeLanguage(path)
		cache.Prepare(index+1, "plain token\n", language, palette, false)
		if len(cache.lines) == 0 {
			t.Errorf("declared language %q has no working lexer", language)
		}
	}
	source := "package main\r\n/* 中文 */\r\nreturn\r\n"
	cache.Prepare(100, source, "go", palette, false)
	line := "/* 中文 */\r"
	if len(cache.lines) == 0 || len(cache.Runs(1, line, nil)) == 0 {
		t.Fatalf("CRLF source has no colored runs: %+v", cache.lines)
	}
	for _, run := range cache.Runs(1, line, nil) {
		if run.Start < 0 || run.End > len(line) || !utf8.ValidString(line[:run.Start]) || !utf8.ValidString(line[:run.End]) {
			t.Fatalf("CRLF run splits Chinese or line ending: %+v", run)
		}
	}
}

func TestCodeEditorPaintsDistinctMultilineContextsWithoutChangingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	source := "package main\n/*\nreturn\n*/\nreturn\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	var colors []ui.Color
	for _, drawn := range driver.Snapshot().Texts {
		if drawn.Value == "return" {
			runs := driver.Snapshot().TextRuns(drawn)
			if len(runs) > 0 {
				colors = append(colors, runs[0].Color)
			}
		}
	}
	if len(colors) != 2 || colors[0] == colors[1] || state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatalf("multiline code colors=%+v source changed=%v", colors, state.session.Current().Dirty())
	}
}

func TestMarkdownCodeRunsKeepUTF8BoundariesAndUnknownLanguagesPlain(t *testing.T) {
	line := "println(\"你好\")"
	runs := markdownCodeRuns("go title=demo", line, nil)
	stringFound := false
	for _, run := range runs {
		if run.Start < 0 || run.End > len(line) || run.End <= run.Start || !utf8.ValidString(line[:run.Start]) || !utf8.ValidString(line[:run.End]) {
			t.Fatalf("rendered code run splits UTF-8: %+v", run)
		}
		if run.Class == components.CodeString && line[run.Start:run.End] == "\"你好\"" {
			stringFound = true
		}
	}
	if !stringFound {
		t.Fatalf("missing Chinese string token: %+v", runs)
	}
	if plain := markdownCodeRuns("unknown-language", line, runs); len(plain) != 0 {
		t.Fatalf("unknown language was highlighted: %+v", plain)
	}
}

func TestMarkdownGoDeclarationsStayPlainWhileCallsAreColored(t *testing.T) {
	for _, test := range []struct {
		line, token string
		want        components.MarkdownCodeClass
	}{
		{"package main", "main", 0},
		{"func main() {", "main", 0},
		{"func (receiver *Thing) Method() {", "Method", 0},
		{"func main() { println(\"hello\") }", "println", components.CodeFunction},
		{"    println(\"hello\")", "println", components.CodeFunction},
	} {
		offset := strings.Index(test.line, test.token)
		if offset < 0 {
			t.Fatal("invalid token fixture")
		}
		var got components.MarkdownCodeClass
		runs := markdownCodeRuns("go", test.line, nil)
		for _, run := range runs {
			if offset >= run.Start && offset < run.End {
				got = run.Class
				break
			}
		}
		if got != test.want {
			t.Errorf("%q %q class=%d, want %d; runs=%+v", test.line, test.token, got, test.want, runs)
		}
	}
}

func TestMarkdownCodeColorsRemainReadableInDarkTheme(t *testing.T) {
	state := newModel()
	state.dark = true
	_, palette := state.look()
	var look components.MarkdownLook
	markdownCodeLook(&look, true, palette)
	for _, class := range []components.MarkdownCodeClass{components.CodeKeyword, components.CodeString, components.CodeNumber, components.CodeFunction} {
		color := look.Code[class]
		if color.Alpha != 1 || color == palette.background || color == palette.ink {
			t.Errorf("dark code class %d is not distinct: %+v", class, color)
		}
	}
}
