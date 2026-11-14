package exporthtml

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderRichHTMLKeepsVisibleLeavesButNotActiveMarkup(t *testing.T) {
	source := []byte(`<p data-id="keep">A &amp; <strong>Bold</strong></p><x-widget attr="private">Visible</x-widget><script>alert(1)</script><p onclick="bad()"><a href="javascript:alert(1)">unsafe</a><img src="https://example.org/p.png" onerror="bad()" alt="remote"></p>`)
	before := append([]byte(nil), source...)
	loads := 0
	output, err := RenderRichHTML(source, func(string) ([]byte, error) { loads++; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"A &amp;", "<strong>Bold</strong>", "Visible", "unsafe", "图片不可用: remote", "原始 HTML", "&lt;script&gt;"} {
		if !strings.Contains(string(output), expected) {
			t.Errorf("rich export omitted %q: %s", expected, output)
		}
	}
	for _, forbidden := range []string{"<script", "<x-widget", `onclick="`, `onerror="`, `href="javascript:`, `src="https://`} {
		if strings.Contains(string(output), forbidden) {
			t.Errorf("unsafe rich markup escaped sanitizer: %q", forbidden)
		}
	}
	if loads != 0 || !bytes.Equal(source, before) {
		t.Fatalf("rich export called image loader or changed source: calls=%d", loads)
	}
	assertInertDOM(t, output)
}

func TestRenderRichHTMLInvalidEncodingFailsWithoutOutput(t *testing.T) {
	output, err := RenderRichHTML([]byte{0xff, '<', 'p', '>'}, nil)
	if err == nil || len(output) != 0 {
		t.Fatalf("invalid HTML source output=%q err=%v", output, err)
	}
}
