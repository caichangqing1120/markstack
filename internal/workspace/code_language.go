package workspace

import (
	"path/filepath"
	"sort"
	"strings"
)

var codeLanguages = []struct{ name, aliases, extensions string }{
	{"go", "golang", "go"},
	{"python", "py", "py pyw"},
	{"javascript", "js jsx", "js jsx mjs cjs"},
	{"typescript", "ts tsx", "ts tsx mts cts"},
	{"sql", "", "sql"},
	{"java", "", "java"},
	{"rust", "rs", "rs"},
	{"cpp", "c++ cc cxx hpp", "cpp cc cxx hpp hh hxx"},
	{"yaml", "yml", "yaml yml"},
	{"dockerfile", "docker", "dockerfile containerfile"},
	{"mysql", "mariadb", "mysql"},
	{"postgresql", "postgres pgsql", "postgresql postgres pgsql"},
	{"sqlite", "sqlite3", ""},
	{"plsql", "oracle", "plsql pls pks pkb"},
	{"tsql", "t-sql", "tsql"},
	{"graphql", "gql", "graphql gql graphqls"},
	{"c", "", "c h"},
	{"csharp", "c# cs", "cs"},
	{"kotlin", "kt kts", "kt kts"},
	{"swift", "", "swift"},
	{"php", "", "php"},
	{"ruby", "rb", "rb"},
	{"scala", "", "scala sc"},
	{"dart", "", "dart"},
	{"lua", "", "lua"},
	{"perl", "pl pm", "pl pm"},
	{"zig", "", "zig"},
	{"html", "htm", "html htm"},
	{"css", "", "css"},
	{"scss", "", "scss"},
	{"sass", "", "sass"},
	{"less", "", "less"},
	{"vue", "vuejs", "vue"},
	{"svelte", "", "svelte"},
	{"json", "jsonc", "json jsonc"},
	{"xml", "svg", "xml svg"},
	{"bash", "sh zsh shell", "bash sh zsh"},
	{"powershell", "ps1 pwsh", "ps1 psm1 psd1"},
	{"toml", "", "toml"},
	{"ini", "cfg conf env", "ini cfg conf env"},
	{"properties", "java-properties", "properties"},
	{"nginx", "nginxconf", "nginx"},
	{"protobuf", "proto", "proto"},
	{"makefile", "make", "mk mak"},
	{"diff", "patch udiff", "diff patch"},
	{"markdown", "md", ""},
	{"regex", "regexp", "regex regexp"},
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
	tag = strings.ToLower(tag)
	if canonical := codeAliases[tag]; canonical != "" {
		return canonical
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
		switch {
		case query == "", language.name == canonical:
			matches = append(matches, match{language.name, 0})
		case strings.HasPrefix(language.name, query):
			matches = append(matches, match{language.name, len(language.name) - len(query)})
		default:
			for alias := range strings.FieldsSeq(language.aliases) {
				if strings.HasPrefix(alias, query) {
					matches = append(matches, match{language.name, len(language.name) + 64})
					break
				}
			}
		}
	}
	if query != "" {
		sort.SliceStable(matches, func(left, right int) bool { return matches[left].rank < matches[right].rank })
	}
	options := make([]string, 0, min(limit, len(matches)))
	for _, result := range matches[:cap(options)] {
		options = append(options, result.name)
	}
	return options
}
