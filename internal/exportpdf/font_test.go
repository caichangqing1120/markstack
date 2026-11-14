package exportpdf

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const notoSansSCDigest = "26305be85f5378dc7d53f3dfa6c852a7e46621b226423400c1821aca9fc01c41"

func TestBundledFontMatchesPinnedRegularInstanceAndRendersChinese(t *testing.T) {
	font, err := BundledFont()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(font)
	if actual := hex.EncodeToString(digest[:]); actual != notoSansSCDigest {
		t.Fatalf("bundled font digest=%s, want %s", actual, notoSansSCDigest)
	}
	data, err := RenderMarkdown([]byte("# 墨栈\n\n跨平台中文 PDF。\n"), nil, font)
	if err != nil {
		t.Fatal(err)
	}
	if text := pdfText(t, data); !strings.Contains(text, "跨平台中文 PDF") {
		t.Fatalf("bundled font lost Chinese: %q", text)
	}
}

func TestBundledFontIncludesReadableOFLNotice(t *testing.T) {
	license := FontLicense()
	if !strings.Contains(license, "SIL OPEN FONT LICENSE Version 1.1") || !strings.Contains(license, "Copyright 2014-2021 Adobe") {
		t.Fatalf("bundled font license is incomplete: %q", license)
	}
}
