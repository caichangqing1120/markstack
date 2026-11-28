package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
	"markstack/internal/workspace"
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

func TestCodeHighlightMainstreamFilesAndFences(t *testing.T) {
	fixtures := []struct{ path, canonical, fence, line string }{
		{"sample.sql", "sql", "SQL", "SELECT '中文';"},
		{"sample.mysql", "mysql", "mysql", "SELECT '中文';"},
		{"sample.pgsql", "postgresql", "postgres", "SELECT '中文'::text;"},
		{"sample.sqlite.sql", "sqlite", "sqlite", "SELECT '中文';"},
		{"sample.plsql", "plsql", "plsql", "SELECT '中文' FROM dual;"},
		{"sample.tsql", "tsql", "tsql", "SELECT '中文';"},
		{"schema.gql", "graphql", "gql", `query { user(name: "中文") { id } }`},
		{"main.go", "go", "Golang", `var note = "中文"`},
		{"Main.JAVA", "java", "java", `public String name = "中文";`},
		{"main.c", "c", "c", `char *value = "中文";`},
		{"main.hpp", "cpp", "C++", `auto value = "中文";`},
		{"Main.cs", "csharp", "c#", `string name = "中文";`},
		{"main.rs", "rust", "rs", `let value = "中文";`},
		{"main.kt", "kotlin", "kt", `val value = "中文"`},
		{"main.swift", "swift", "swift", `let value = "中文"`},
		{"main.php", "php", "php", `<?php echo "中文"; ?>`},
		{"main.rb", "ruby", "rb", `puts "中文"`},
		{"main.py", "python", "py", `print("中文")`},
		{"main.scala", "scala", "scala", `val label = "中文"`},
		{"main.dart", "dart", "dart", `final label = "中文";`},
		{"main.lua", "lua", "lua", `local label = "中文"`},
		{"main.pl", "perl", "perl", `print "中文";`},
		{"main.zig", "zig", "zig", `const label = "中文";`},
		{"main.jsx", "javascript", "jsx", `const label = "中文";`},
		{"main.tsx", "typescript", "tsx", `const label: string = "中文";`},
		{"index.htm", "html", "htm", `<p title="中文">Text</p>`},
		{"main.css", "css", "css", `p { content: "中文"; }`},
		{"main.scss", "scss", "scss", `$name: "中文";`},
		{"main.sass", "sass", "sass", `$name: "中文"`},
		{"main.less", "less", "less", `@name: "中文";`},
		{"view.vue", "vue", "vue", `<template><p title="中文" /></template>`},
		{"view.svelte", "svelte", "svelte", `<p title="中文">{value}</p>`},
		{"data.jsonc", "json", "jsonc", `{"name":"中文"}`},
		{"icon.svg", "xml", "svg", `<svg title="中文"/>`},
		{"build.zsh", "bash", "shell", `echo "中文"`},
		{"install.ps1", "powershell", "ps1", `Write-Output "中文"`},
		{"config.yml", "yaml", "yml", `title: "中文"`},
		{"config.toml", "toml", "toml", `title = "中文"`},
		{"config.ini", "ini", "ini", "[中文]"},
		{"config.properties", "properties", "properties", "# 中文"},
		{".env.local", "ini", "env", `NAME="中文"`},
		{"Dockerfile.dev", "dockerfile", "docker", `RUN echo "中文"`},
		{"nginx.conf", "nginx", "nginx", `return 200 "中文";`},
		{"schema.proto", "protobuf", "proto", `syntax = "proto3"; // 中文`},
		{"GNUmakefile", "makefile", "make", "# 中文"},
		{"fix.patch", "diff", "patch", "+中文"},
		{"match.regex", "regex", "regex", `^[中文]+\d{2}$`},
		{"", "markdown", "md", "# 中文"},
	}
	state := newModel()
	_, palette := state.look()
	for _, fixture := range fixtures {
		t.Run(fixture.path+"/"+fixture.fence, func(t *testing.T) {
			validRange := func(start, end int) bool {
				return start >= 0 && end > start && end <= len(fixture.line) &&
					utf8.ValidString(fixture.line[:start]) && utf8.ValidString(fixture.line[:end])
			}
			if fixture.path != "" {
				language := codeLanguage(fixture.path)
				if language != fixture.canonical || workspace.KindOf(fixture.path) != workspace.CodeKind {
					t.Errorf("file language=%q, kind=%v; want %q and code", language, workspace.KindOf(fixture.path), fixture.canonical)
				}
				var cache codeHighlightCache
				cache.Prepare(1, fixture.line+"\r\n", language, palette, false)
				colored := false
				line := fixture.line + "\r"
				for _, run := range cache.Runs(0, line, nil) {
					if run.Start < 0 || run.End <= run.Start || run.End > len(line) ||
						!utf8.ValidString(line[:run.Start]) || !utf8.ValidString(line[:run.End]) {
						t.Fatalf("invalid file run: %+v", run)
					}
					colored = colored || run.Color != palette.ink
				}
				if !colored {
					t.Error("file has no syntax color")
				}
			}
			for _, tag := range []string{fixture.canonical, fixture.fence} {
				runs := markdownCodeRuns(tag+" title=demo", fixture.line, nil)
				if len(runs) == 0 {
					t.Errorf("fence %q has no syntax color", tag)
				}
				for _, run := range runs {
					if !validRange(run.Start, run.End) || run.Class == 0 {
						t.Fatalf("invalid fence run: %+v", run)
					}
				}
			}
		})
	}
}

func TestFenceLanguageSuggestionsFilterAliasesAndPreserveSource(t *testing.T) {
	all := workspace.CodeLanguageSuggestions("", 100)
	if len(all) < 30 {
		t.Fatalf("only %d code language suggestions", len(all))
	}
	for query, want := range map[string]string{
		"SQL": "sql", "jav": "java", "rs": "rust", "c++": "cpp", "yml": "yaml",
		"dock": "dockerfile", "ngi": "nginx", "patch": "diff", "pgs": "postgresql", "ps1": "powershell",
	} {
		if results := workspace.CodeLanguageSuggestions(query, 8); len(results) == 0 || results[0] != want {
			t.Errorf("suggestions for %q = %q, want %q first", query, results, want)
		}
	}
	if results := workspace.CodeLanguageSuggestions("made-up-language", 8); len(results) != 0 {
		t.Fatalf("unrecognized input unexpectedly matches %q", results)
	}

	const source = "```go title=demo\r\nSELECT '中文';\r\n```\r\n"
	path := filepath.Join(t.TempDir(), "language.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf"+source), 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, codeLanguageButtons.At(0))
	if err := driver.Type("pgs"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source {
		t.Fatal("filtering changed the source before confirmation")
	}
	clickID(t, driver, codeLanguageSuggestions.At(0))
	want := strings.Replace(source, "```go", "```postgresql", 1)
	if state.session.Current().Text() != want || driver.Snapshot().Focused != codeInputID {
		t.Fatalf("accepting suggestion gave source=%q focus=%v", state.session.Current().Text(), driver.Snapshot().Focused)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if disk, err := os.ReadFile(path); err != nil || string(disk) != "\xef\xbb\xbf"+want {
		t.Fatalf("saved fence lost BOM, attributes or line endings: %q, %v", disk, err)
	}
	clickID(t, driver, codeLanguageButtons.At(0))
	if err := driver.Type("c++"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyDown, 0); err != nil || driver.Snapshot().Focused != codeLanguageInputID {
		t.Fatalf("Down moved native focus away from the search field: %v", err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != strings.Replace(source, "```go", "```cpp", 1) {
		t.Fatalf("keyboard confirmation did not apply the canonical alias: %q", got)
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

func TestCodeHighlightCacheRefreshesPaletteWithoutSourceChange(t *testing.T) {
	state := newModel()
	_, palette := state.look()
	var cache codeHighlightCache
	const source = "package main\n"
	cache.Prepare(7, source, "go", palette, false)
	changed := palette
	changed.accent = ui.RGB(10, 131, 50)
	cache.Prepare(7, source, "go", changed, false)
	runs := cache.Runs(0, "package main", nil)
	if len(runs) == 0 || runs[0].Color != changed.accent {
		t.Fatalf("unchanged source retained old keyword color: runs=%+v want=%+v", runs, changed.accent)
	}
	first := &cache.lines[0][0]
	cache.Prepare(7, source, "go", changed, false)
	if &cache.lines[0][0] != first {
		t.Fatal("unchanged palette rebuilt highlight runs")
	}
}

func TestCodeHighlightThemeToggleUsesCurrentPalette(t *testing.T) {
	for _, initialDark := range []bool{false, true} {
		name := "light"
		if initialDark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "palette.go")
			body := []byte("package main\n\nfunc main() { println(123) }\n")
			if err := os.WriteFile(filename, body, 0640); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.dark, state.preferences.Dark = initialDark, initialDark
			state.openFile(filename)
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			for toggle := 0; toggle < 2; toggle++ {
				clickID(t, driver, themeToggleID)
				if err := driver.Resize(740, 520); err != nil {
					t.Fatal(err)
				}
				_, palette := state.look()
				keyword := state.codeHighlight.Runs(0, "package main", nil)
				punctuation := state.codeHighlight.Runs(2, "func main() { println(123) }", nil)
				var punctuationColor ui.Color
				for _, run := range punctuation {
					if run.Start <= 9 && run.End > 9 {
						punctuationColor = run.Color
					}
				}
				if len(keyword) == 0 || keyword[0].Color != palette.accent || punctuationColor != palette.ink {
					t.Errorf("toggle %d dark=%v keyword=%+v want=%+v punctuation=%+v want=%+v", toggle, state.dark, keyword, palette.accent, punctuationColor, palette.ink)
				}
				paintedKeyword := false
				for _, drawn := range driver.Snapshot().Texts {
					if drawn.Value != "package main" {
						continue
					}
					for _, run := range driver.Snapshot().TextRuns(drawn) {
						if run.Column == 0 && run.Bytes >= 7 && run.Color == palette.accent {
							paintedKeyword = true
						}
					}
				}
				if !paintedKeyword {
					t.Errorf("toggle %d did not paint the current keyword color", toggle)
				}
				current := state.session.Current()
				disk, err := os.ReadFile(filename)
				if err != nil || current.Text() != string(body) || current.Dirty() || !bytes.Equal(disk, body) {
					t.Fatalf("theme toggle changed document or disk: text=%q dirty=%v disk=%q err=%v", current.Text(), current.Dirty(), disk, err)
				}
			}
		})
	}
}

func TestCodeHighlightThemeWriteFailureKeepsCurrentPalette(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "palette.go")
	const source = "package main\n"
	if err := os.WriteFile(filename, []byte(source), 0640); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(filename)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(t.TempDir(), "loose", "settings.json")
	if err := os.MkdirAll(filepath.Dir(unsafe), 0755); err != nil {
		t.Fatal(err)
	}
	state.settingsStore = &session.PreferencesStore{Path: unsafe}
	clickID(t, driver, themeToggleID)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	_, palette := state.look()
	runs := state.codeHighlight.Runs(0, "package main", nil)
	if state.dark || state.preferences.Dark || state.message == "" || len(runs) == 0 || runs[0].Color != palette.accent {
		t.Fatalf("failed settings write changed theme or highlight: dark=%v preferences=%+v message=%q runs=%+v", state.dark, state.preferences, state.message, runs)
	}
	bytesOnDisk, err := os.ReadFile(filename)
	if err != nil || string(bytesOnDisk) != source || state.session.Current().Dirty() {
		t.Fatalf("failed settings write changed document: disk=%q dirty=%v err=%v", bytesOnDisk, state.session.Current().Dirty(), err)
	}
}
