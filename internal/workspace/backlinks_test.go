package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func referenceFixture(t *testing.T, root, relative, value string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBacklinksMarkdownSourcePositionsAndFiltering(t *testing.T) {
	root := t.TempDir()
	target := referenceFixture(t, root, "docs/目标 File.md", "[self](<目标 File.md>)\n")
	source := "\ufeff封面\r\n\r\n- 从这里 [阅读](../docs/%E7%9B%AE%E6%A0%87%20File.md#title)\r\n继续 [定义][ref] 与 [第二个](<../docs\\目标 File.md?raw=1#heading>)\r\n\r\n[ref]: <../docs/目标 File.md>\r\n\r\n`[inline][ref]`\r\n\r\n```md\r\n[fenced][ref]\r\n```\r\n\r\n![image][ref]\r\n[external](https://example.net/docs/file.md)"
	referenceFixture(t, root, "notes/from.md", source)
	for _, relative := range []string{".hidden/skip.md", "node_modules/skip.md", "vendor/skip.md", "skip.txt"} {
		referenceFixture(t, root, relative, "[link](</docs/目标 File.md>) [link](<../docs/目标 File.md>)")
	}
	rows, incomplete, err := Backlinks(context.Background(), root, target)
	if err != nil || incomplete || len(rows) != 4 {
		t.Fatalf("backlinks: %+v, incomplete=%v, err=%v", rows, incomplete, err)
	}
	if rows[0].Relative != "docs/目标 File.md" || rows[0].Line != 1 {
		t.Fatalf("explicit relative self-link: %+v", rows[0])
	}
	for index, want := range []struct {
		line  int
		label string
	}{{3, "[阅读]"}, {4, "[定义]"}, {4, "[第二个]"}} {
		hit := rows[index+1]
		if hit.Relative != "notes/from.md" || hit.Line != want.line || !strings.Contains(hit.Context, want.label) || !strings.HasPrefix(strings.TrimPrefix(source, "\ufeff")[hit.ByteOffset:], want.label) {
			t.Fatalf("hit %d: %+v, want %+v", index, hit, want)
		}
	}
}

func TestBacklinksLimitAndCancellation(t *testing.T) {
	root := t.TempDir()
	target := referenceFixture(t, root, "target.md", "target")
	referenceFixture(t, root, "refs.md", strings.Repeat("[entry](target.md)\n\n", referenceResultLimit+1))
	rows, incomplete, err := Backlinks(context.Background(), root, target)
	if err != nil || !incomplete || len(rows) != referenceResultLimit {
		t.Fatalf("count=%d incomplete=%v err=%v", len(rows), incomplete, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Backlinks(ctx, root, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	for _, raw := range []string{"/docs/page.md", "//example.net/page.md", "https://example.net/page.md", "../../escape.md", "%2fpage.md", "C:\\page.md"} {
		if candidate := RelativeReferencePath("notes/current.md", raw); candidate != "" {
			t.Errorf("%q unexpectedly resolves to %s", raw, candidate)
		}
	}
}
