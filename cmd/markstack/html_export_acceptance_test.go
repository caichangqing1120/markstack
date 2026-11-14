package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	htmlnode "golang.org/x/net/html"
)

func exportSamplePNG(t *testing.T) []byte {
	t.Helper()
	bitmap := image.NewNRGBA(image.Rect(0, 0, 240, 80))
	for row := 0; row < 80; row++ {
		for column := 0; column < 240; column++ {
			value := color.NRGBA{R: 38, G: 123, B: 145, A: 255}
			if column >= 80 && column < 160 {
				value = color.NRGBA{R: 91, G: 165, B: 102, A: 255}
			} else if column >= 160 {
				value = color.NRGBA{R: 231, G: 147, B: 70, A: 255}
			}
			bitmap.SetNRGBA(column, row, value)
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, bitmap); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestHTMLExportArtifacts(t *testing.T) {
	artifacts := os.Getenv("MARKSTACK_EXPORT_ARTIFACT_DIR")
	if artifacts == "" {
		t.Skip("acceptance artifact directory not set")
	}
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "sample.png"), exportSamplePNG(t), 0644); err != nil {
		t.Fatal(err)
	}
	markdownPath := filepath.Join(root, "Markdown.md")
	markdownSource := "# 导出示例\n\n**粗体**与安全[链接](https://example.org)。\n\n- [x] 待办已完成\n\n![本地图](assets/sample.png)\n\n| 列 | 值 |\n| --- | --- |\n| 一 | 二 |\n\n<script>alert(1)</script>\n"
	if err := os.WriteFile(markdownPath, []byte(markdownSource), 0644); err != nil {
		t.Fatal(err)
	}
	richPath := filepath.Join(root, "Rich.mh")
	richSource := `<h1>富文本导出</h1><p>本地图片 <img src="assets/sample.png" alt="本地图"></p><x-widget>未知结构</x-widget><script>alert(1)</script>`
	if err := os.WriteFile(richPath, []byte(richSource), 0644); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct {
		path, target, menuShot, source string
	}{
		{markdownPath, "Markdown.html", "markdown-export-menu-740.png", markdownSource},
		{richPath, "Rich.html", "rich-export-menu-740.png", richSource},
	} {
		state := newModel()
		var canvas gfx.Canvas
		session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		state.openFile(candidate.path)
		if err := session.Resize(740, 520); err != nil {
			session.Close()
			t.Fatal(err)
		}
		clickID(t, session.Driver(), fileMenuID)
		// The file menu slides in for 150ms; its labels are painted at their
		// animated offset while the row hits keep the resting geometry, so check
		// layout and take the artifact only once the entrance has finished.
		if err := session.Rest(); err != nil {
			session.Close()
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(artifacts, candidate.menuShot)); err != nil {
			session.Close()
			t.Fatal(err)
		}
		for _, problem := range headless.CheckLayout(session.Snapshot()) {
			if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
				session.Close()
				t.Fatalf("export menu layout=%+v", problem)
			}
		}
		target := filepath.Join(artifacts, candidate.target)
		if err := session.Driver().QueueSaveFile(target, nil); err != nil {
			session.Close()
			t.Fatal(err)
		}
		clickID(t, session.Driver(), menuExportHTMLID)
		session.Close()
		data, err := os.ReadFile(target)
		if err != nil || !bytes.Contains(data, []byte("data:image/png;base64,")) || bytes.Contains(data, []byte("<script")) {
			t.Fatalf("export artifact %q=%d bytes, %v", target, len(data), err)
		}
		assertExportedHTML(t, data)
		original, err := os.ReadFile(candidate.path)
		if err != nil || string(original) != candidate.source {
			t.Fatalf("export changed source: %q, %v", original, err)
		}
	}
}

func assertExportedHTML(t *testing.T, data []byte) {
	t.Helper()
	root, err := htmlnode.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var inspect func(*htmlnode.Node)
	inspect = func(node *htmlnode.Node) {
		if node.Type == htmlnode.ElementNode {
			if node.Data == "script" || node.Data == "iframe" || node.Data == "form" || node.Data == "object" {
				t.Errorf("active export element %q", node.Data)
			}
			for _, attribute := range node.Attr {
				if len(attribute.Key) > 1 && attribute.Key[:2] == "on" {
					t.Errorf("active export attribute %q", attribute.Key)
				}
				if attribute.Key == "src" && !bytes.HasPrefix([]byte(attribute.Val), []byte("data:image/png;base64,")) && !bytes.HasPrefix([]byte(attribute.Val), []byte("data:image/jpeg;base64,")) && !bytes.HasPrefix([]byte(attribute.Val), []byte("data:image/gif;base64,")) {
					t.Errorf("external export image %q", attribute.Val)
				}
				if attribute.Key == "href" {
					parsed, err := url.Parse(attribute.Val)
					if err != nil || parsed.Scheme != "https" && parsed.Scheme != "http" && parsed.Scheme != "mailto" {
						t.Errorf("unsafe export link %q", attribute.Val)
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			inspect(child)
		}
	}
	inspect(root)
}
