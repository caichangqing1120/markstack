package workspace

import (
	"path/filepath"
	"sort"
	"strings"
)

var codeLanguages = []struct{ name, display, aliases, extensions string }{
	{"go", "Go", "golang", "go"},
	{"python", "Python", "py", "py pyw"},
	{"sql", "SQL", "", "sql"},
	{"javascript", "JavaScript", "js jsx", "js jsx mjs cjs"},
	{"typescript", "TypeScript", "ts tsx", "ts tsx mts cts"},
	{"java", "Java", "", "java"},
	{"bash", "Bash", "sh zsh shell", "bash sh zsh"},
	{"json", "JSON", "jsonc", "json jsonc"},
	{"yaml", "YAML", "yml", "yaml yml"},
	{"html", "HTML", "htm", "html htm"},
	{"css", "CSS", "", "css"},
	{"rust", "Rust", "rs", "rs"},
	{"cpp", "C++", "c++ cc cxx hpp", "cpp cc cxx hpp hh hxx"},
	{"dockerfile", "Dockerfile", "docker", "dockerfile containerfile"},
	{"mermaid", "Mermaid", "flowchart sequence", "mmd mermaid"},
	{"mysql", "MySQL", "mariadb", "mysql"},
	{"postgresql", "PostgreSQL", "postgres pgsql pg", "postgresql postgres pgsql"},
	{"sqlite", "SQLite", "sqlite3", ""},
	{"plsql", "PL/SQL", "oracle", "plsql pls pks pkb"},
	{"tsql", "T-SQL", "t-sql", "tsql"},
	{"graphql", "GraphQL", "gql", "graphql gql graphqls"},
	{"c", "C", "", "c h"},
	{"csharp", "C#", "c# cs", "cs"},
	{"kotlin", "Kotlin", "kt kts", "kt kts"},
	{"swift", "Swift", "", "swift"},
	{"php", "PHP", "", "php"},
	{"ruby", "Ruby", "rb", "rb"},
	{"scala", "Scala", "", "scala sc"},
	{"dart", "Dart", "", "dart"},
	{"lua", "Lua", "", "lua"},
	{"perl", "Perl", "pl pm", "pl pm"},
	{"zig", "Zig", "", "zig"},
	{"scss", "SCSS", "", "scss"},
	{"sass", "Sass", "", "sass"},
	{"less", "Less", "", "less"},
	{"vue", "Vue", "vuejs", "vue"},
	{"svelte", "Svelte", "", "svelte"},
	{"xml", "XML", "svg", "xml svg"},
	{"powershell", "PowerShell", "ps1 pwsh", "ps1 psm1 psd1"},
	{"toml", "TOML", "", "toml"},
	{"ini", "INI", "cfg conf env", "ini cfg conf env"},
	{"properties", "Properties", "java-properties", "properties"},
	{"nginx", "Nginx", "nginxconf", "nginx"},
	{"protobuf", "Protobuf", "proto", "proto"},
	{"makefile", "Makefile", "make", "mk mak"},
	{"diff", "Diff", "patch udiff", "diff patch"},
	{"markdown", "Markdown", "md", ""},
	{"regex", "Regex", "regexp", "regex regexp"},
	{"latex", "LaTeX", "tex math", "tex latex"},
	{"plaintext", "Plain text", "text plain txt 纯文本", ""},
}

var codeExtensions, codeAliases = func() (map[string]string, map[string]string) {
	extensions, aliases := make(map[string]string), make(map[string]string)
	for _, language := range codeLanguages {
		for _, extension := range strings.Fields(language.extensions) {
			extensions["."+extension] = language.name
		}
		for _, alias := range strings.Fields(language.aliases) {
			aliases[alias] = language.name
		}
	}
	return extensions, aliases
}()

func CodeLanguage(path string) string {
	name := strings.ToLower(filepath.Base(path))
	switch name {
	case "makefile", "gnumakefile":
		return "makefile"
	case "nginx.conf":
		return "nginx"
	case ".editorconfig", ".gitconfig":
		return "ini"
	}
	for _, dialect := range []string{"mysql", "postgresql", "postgres", "pgsql", "sqlite", "plsql", "tsql"} {
		if strings.HasSuffix(name, "."+dialect+".sql") {
			return NormalizeCodeLanguage(dialect)
		}
	}
	if strings.HasPrefix(name, ".env.") {
		return "ini"
	}
	prefix, _, _ := strings.Cut(name, ".")
	if prefix == "dockerfile" || prefix == "containerfile" {
		return "dockerfile"
	}
	return codeExtensions[filepath.Ext(name)]
}

func NormalizeCodeLanguage(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if canonical := codeAliases[tag]; canonical != "" {
		return canonical
	}
	return tag
}

func CodeLanguageDisplayName(tag string) string {
	canonical := NormalizeCodeLanguage(tag)
	if canonical == "" {
		canonical = "plaintext"
	}
	for _, language := range codeLanguages {
		if language.name == canonical {
			return language.display
		}
	}
	return tag
}

func CodeLanguageSuggestions(query string, limit int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		return nil
	}
	type match struct {
		name string
		rank int
	}
	var matches []match
	canonical := NormalizeCodeLanguage(query)
	for _, language := range codeLanguages {
		if query == "" && language.name == "plaintext" {
			continue
		}
		if query == "" || language.name == canonical {
			matches = append(matches, match{language.name, 0})
			continue
		}
		rank := min(codeLanguageMatchRank(language.name, query), codeLanguageMatchRank(strings.ToLower(language.display), query))
		for alias := range strings.FieldsSeq(language.aliases) {
			rank = min(rank, codeLanguageMatchRank(alias, query))
		}
		if rank < 3 {
			matches = append(matches, match{language.name, rank})
		}
	}
	if query != "" {
		sort.SliceStable(matches, func(left, right int) bool {
			first, second := matches[left], matches[right]
			if first.rank != second.rank {
				return first.rank < second.rank
			}
			return first.rank == 1 && len(first.name) < len(second.name)
		})
	}
	options := make([]string, 0, min(limit, len(matches)))
	for _, result := range matches[:cap(options)] {
		options = append(options, result.name)
	}
	return options
}

func codeLanguageMatchRank(value, query string) int {
	switch {
	case value == query:
		return 0
	case strings.HasPrefix(value, query):
		return 1
	case strings.Contains(value, query):
		return 2
	default:
		return 3
	}
}
