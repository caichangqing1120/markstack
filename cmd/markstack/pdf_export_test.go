package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
)

func requirePDFFont(t *testing.T) {
	t.Helper()
	t.Setenv("MARKSTACK_PDF_FONT", "")
	if _, err := loadPDFFont(); err != nil {
		t.Fatal(err)
	}
}

func TestPDFExportMarkdownPreservesDirtyDraftAndDisk(t *testing.T) {
	requirePDFFont(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.session.Current().Replace("# 草稿\n\n正文中文\n")
	before := state.session.RecoverySnapshot()
	target := filepath.Join(root, "export.pdf")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	output, err := os.ReadFile(target)
	if err != nil || !bytes.HasPrefix(output, []byte("%PDF-")) {
		t.Fatalf("PDF export=%q, %v, message=%q", output[:min(len(output), 12)], err, state.message)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "# Original\n" {
		t.Fatalf("PDF export changed source: %q, %v", data, err)
	}
	current := state.session.Current()
	if !current.Dirty() || current.Path != path || current.Text() != before.Drafts[0].Body || state.session.RecoverySnapshot().Drafts[0].Body != before.Drafts[0].Body {
		t.Fatalf("PDF export changed draft or tab: path=%q dirty=%v", current.Path, current.Dirty())
	}
}

func TestPDFExportRichHTMLPreservesUnknownSource(t *testing.T) {
	requirePDFFont(t)
	path := filepath.Join(t.TempDir(), "note.mh")
	source := `<p>正文</p><script>alert(1)</script><!--keep-->`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	target := filepath.Join(t.TempDir(), "rich.pdf")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	output, err := os.ReadFile(target)
	if err != nil || !bytes.HasPrefix(output, []byte("%PDF-")) || bytes.Contains(output, []byte("/JavaScript")) {
		t.Fatalf("rich PDF=%q, %v", output[:min(len(output), 12)], err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != source || state.session.Current().Dirty() {
		t.Fatalf("rich source changed: %q, %v", data, err)
	}
}

func TestPDFExportUnsupportedTextPreservesDraftAndPicker(t *testing.T) {
	for _, fixture := range []struct {
		extension, original, draft, unsupported, codepoint string
	}{
		{".md", "# original\r\n", "# Draft\r\n\r\nBefore **\U0001f642** after.\r\n", "\U0001f642", "U+1F642"},
		{".mh", "<p>original</p>\r\n", "<table><tr><td>\U0002000b</td><td>tail</td></tr></table>\r\n", "\U0002000b", "U+2000B"},
	} {
		t.Run(fixture.extension, func(t *testing.T) {
			requirePDFFont(t)
			root := t.TempDir()
			path := filepath.Join(root, "note"+fixture.extension)
			original := []byte("\xef\xbb\xbf" + fixture.original)
			if err := os.WriteFile(path, original, 0o640); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			if !state.openFile(path) {
				t.Fatalf("open source: %s", state.message)
			}
			document := state.session.Current()
			document.Replace(fixture.draft)
			document.Editor.SetSelection(text.Range{Start: 3, End: 1})
			selection := document.Editor.Selection()
			before := state.session.RecoverySnapshot()
			draft := document.Text()
			target := filepath.Join(root, "export.pdf")
			if err := driver.QueueSaveFile(target, nil); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, fileMenuID)
			clickID(t, driver, menuExportPDFID)
			if !strings.Contains(state.message, fixture.codepoint) {
				t.Fatalf("missing unsupported-character message: %q", state.message)
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("refused export touched the destination: %v", err)
			}
			if state.session.Current() != document || !document.Dirty() || document.Text() != draft || document.Editor.Selection() != selection || !reflect.DeepEqual(state.session.RecoverySnapshot(), before) {
				t.Fatal("refused export changed the draft, tab, selection or recovery snapshot")
			}
			if stored, err := os.ReadFile(path); err != nil || !bytes.Equal(stored, original) {
				t.Fatalf("refused export changed source bytes: %v", err)
			}
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("refused export changed source permissions: %v", err)
			}
			document.Replace(strings.Replace(fixture.draft, fixture.unsupported, "\u4e2d\u6587", 1))
			clickID(t, driver, fileMenuID)
			clickID(t, driver, menuExportPDFID)
			data, err := os.ReadFile(target)
			if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
				t.Fatalf("corrected export did not use the original destination: %v, status=%q", err, state.message)
			}
		})
	}
}

func TestPDFExportIncompleteOptionalFontPreservesDraftAndPicker(t *testing.T) {
	requirePDFFont(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.mh")
	original := []byte("\xef\xbb\xbf<p>original</p>\r\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	fontPath := filepath.Join(root, "selected.ttf")
	font := []byte("no")
	if err := os.WriteFile(fontPath, font, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MARKSTACK_PDF_FONT", fontPath)
	state, driver := testDriver(t)
	if !state.openFile(path) {
		t.Fatalf("open source: %s", state.message)
	}
	document := state.session.Current()
	document.Replace("<p>draft 中文</p>\r\n")
	document.Editor.SetSelection(text.Range{Start: 3, End: 1})
	selection := document.Editor.Selection()
	before := state.session.RecoverySnapshot()
	target := filepath.Join(root, "export.pdf")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if !strings.Contains(state.message, "TrueType") {
		t.Fatalf("expected a TrueType error, got %q", state.message)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("refused export touched the destination: %v", err)
	}
	if state.session.Current() != document || !document.Dirty() || document.Editor.Selection() != selection || !reflect.DeepEqual(state.session.RecoverySnapshot(), before) {
		t.Fatal("refused export changed the draft, tab, selection or recovery snapshot")
	}
	t.Setenv("MARKSTACK_PDF_FONT", "")
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	data, err := os.ReadFile(target)
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("bundled-font retry did not use the original destination: %v, status=%q", err, state.message)
	}
	for _, fixture := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{{path, original, 0o640}, {fontPath, font, 0o600}} {
		if stored, err := os.ReadFile(fixture.path); err != nil || !bytes.Equal(stored, fixture.data) {
			t.Fatalf("export changed input bytes: %v", err)
		}
		if info, err := os.Stat(fixture.path); err != nil || info.Mode().Perm() != fixture.mode {
			t.Fatalf("export changed input permissions: %v", err)
		}
	}
}

func TestPDFExportCancelExistingTargetAndExternalConflict(t *testing.T) {
	requirePDFFont(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.session.Current().Replace("# Draft\n")
	if err := driver.QueueSaveFile("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if !state.session.Current().Dirty() || state.session.Current().Text() != "# Draft\n" {
		t.Fatal("canceled PDF export changed draft")
	}
	target := filepath.Join(root, "existing.pdf")
	if err := os.WriteFile(target, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if data, err := os.ReadFile(target); err != nil || string(data) != "keep" || state.message == "" {
		t.Fatalf("existing PDF target=%q, %v, message=%q", data, err, state.message)
	}
	outside := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked.pdf")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueSaveFile(linked, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if data, err := os.ReadFile(outside); err != nil || string(data) != "private" || state.message == "" {
		t.Fatalf("symlink PDF target=%q, %v, message=%q", data, err, state.message)
	}
	if err := os.WriteFile(path, []byte("# External\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(root, "fresh.pdf")
	if err := driver.QueueSaveFile(fresh, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if _, err := os.Stat(fresh); !os.IsNotExist(err) || state.session.Current().Text() != "# Draft\n" {
		t.Fatalf("external conflict wrote PDF or changed draft: %v", err)
	}
}

func TestPDFExportRejectsSymlinkSwappedSource(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.session.Current().Replace("# Draft\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	outsideSource := filepath.Join(root, "outside-source.md")
	if err := os.WriteFile(outsideSource, []byte("# Outside\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSource, path); err != nil {
		t.Fatal(err)
	}
	swapTarget := filepath.Join(root, "swapped.pdf")
	if err := driver.QueueSaveFile(swapTarget, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if _, err := os.Stat(swapTarget); !os.IsNotExist(err) || state.message == "" {
		t.Fatalf("symlink-swapped source wrote PDF: %v message=%q", err, state.message)
	}
	if data, err := os.ReadFile(outsideSource); err != nil || string(data) != "# Outside\n" {
		t.Fatalf("symlink-swapped source target changed: %q, %v", data, err)
	}
	if state.session.Current().Text() != "# Draft\n" || !state.session.Current().Dirty() {
		t.Fatal("symlink-swapped source changed draft")
	}
}

func TestPDFExportUnsupportedKindsAndMissingFontAreDisabledOrRejected(t *testing.T) {
	for _, extension := range []string{"txt", "go"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note."+extension)
			if err := os.WriteFile(path, []byte("literal"), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			clickID(t, driver, fileMenuID)
			found := false
			for _, hit := range driver.Snapshot().Hits {
				if hit.ID == menuExportPDFID {
					found = true
					if !hit.Disabled {
						t.Fatalf("PDF export enabled for %s", extension)
					}
				}
			}
			if !found {
				t.Fatal("PDF menu item missing")
			}
		})
	}
	requirePDFFont(t)
	state, driver := testDriver(t)
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("# Note"), 0644); err != nil {
		t.Fatal(err)
	}
	state.openFile(path)
	t.Setenv("MARKSTACK_PDF_FONT", filepath.Join(t.TempDir(), "missing.ttf"))
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if !strings.Contains(state.message, "字体") {
		t.Fatalf("missing PDF font message=%q", state.message)
	}
}

func TestPDFExportRefusesUncommittedIMEComposition(t *testing.T) {
	requirePDFFont(t)
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("# 原文\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	current := state.session.Current()
	current.Editor.SetMarkedText("pin", text.Range{Start: 3, End: 3})
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if !strings.Contains(state.message, "输入法") || current.Dirty() {
		t.Fatalf("preedit export message=%q dirty=%v", state.message, current.Dirty())
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "# 原文\n" {
		t.Fatalf("preedit export changed source: %q, %v", data, err)
	}
}

func TestPDFExportRefusesRichHTMLComposition(t *testing.T) {
	requirePDFFont(t)
	path := filepath.Join(t.TempDir(), "note.mh")
	if err := os.WriteFile(path, []byte("<p>原文</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	if !strings.Contains(state.message, "输入法") || state.session.Current().Dirty() || state.session.Current().Text() != "<p>原文</p>" {
		t.Fatalf("rich preedit export message=%q dirty=%v source=%q", state.message, state.session.Current().Dirty(), state.session.Current().Text())
	}
}

func TestPDFExportDefaultUsesBundledUnicodeFont(t *testing.T) {
	t.Setenv("MARKSTACK_PDF_FONT", "")
	font, err := loadPDFFont()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(font)
	if actual := hex.EncodeToString(digest[:]); actual != "26305be85f5378dc7d53f3dfa6c852a7e46621b226423400c1821aca9fc01c41" {
		t.Fatalf("default PDF font=%s, want verified Noto Sans SC", actual)
	}
}

// exportedPDF is the result of inspecting an exported PDF in Go. poppler is
// not part of the test environment, so the test parses the objects, decodes the
// page content streams and reads the UTF-16BE strings fpdf writes for its
// embedded UTF-8 font. That proves every page, not only the first.
type exportedPDF struct {
	mediaWidth, mediaHeight float64
	pageTexts               [][]string
	images                  [][]exportedPDFImage
	fontFile                []byte
	typeZero, toUnicode     bool
}

type exportedPDFImage struct {
	name       string
	x, y, w, h float64
}

var (
	exportedPDFObject  = regexp.MustCompile(`(?s)(\d+)\s+0\s+obj\b`)
	exportedPDFPage    = regexp.MustCompile(`/Type\s*/Page\b`)
	exportedPDFPages   = regexp.MustCompile(`/Type\s*/Pages\b`)
	exportedPDFMedia   = regexp.MustCompile(`/MediaBox\s*\[\s*[\d.]+\s+[\d.]+\s+([\d.]+)\s+([\d.]+)\s*\]`)
	exportedPDFRef     = regexp.MustCompile(`(\d+)\s+0\s+R`)
	exportedPDFKids    = regexp.MustCompile(`/Kids\s*\[([^\]]*)\]`)
	exportedPDFCount   = regexp.MustCompile(`/Count\s+(\d+)`)
	exportedPDFImageOp = regexp.MustCompile(`q\s+([\d.]+)\s+0\s+0\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+cm\s+/(\S+)\s+Do\s+Q`)
	exportedPDFFont    = regexp.MustCompile(`/FontFile2\s+(\d+)\s+0\s+R`)
)

func inspectExportedPDF(t *testing.T, data []byte) exportedPDF {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(data), []byte("%%EOF")) {
		t.Fatal("exported file is not a complete PDF")
	}
	type object struct {
		dictionary string
		decoded    []byte
	}
	objects := map[int]object{}
	var order []int
	starts := exportedPDFObject.FindAllSubmatchIndex(data, -1)
	for index, match := range starts {
		number := 0
		fmt.Sscanf(string(data[match[2]:match[3]]), "%d", &number)
		end := len(data)
		if index+1 < len(starts) {
			end = starts[index+1][0]
		}
		chunk := data[match[1]:end]
		value := object{dictionary: string(chunk)}
		marker, markerLength := bytes.Index(chunk, []byte("stream\r\n")), len("stream\r\n")
		if marker < 0 {
			marker, markerLength = bytes.Index(chunk, []byte("stream\n")), len("stream\n")
		}
		if marker >= 0 {
			value.dictionary = string(chunk[:marker])
			raw := chunk[marker+markerLength:]
			if stop := bytes.LastIndex(raw, []byte("endstream")); stop >= 0 {
				raw = bytes.TrimRight(raw[:stop], "\r\n")
				if bytes.Contains([]byte(value.dictionary), []byte("/FlateDecode")) {
					if reader, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
						value.decoded, _ = io.ReadAll(reader)
						reader.Close()
					}
				}
			}
		}
		objects[number] = value
		order = append(order, number)
	}
	pdf := exportedPDF{typeZero: bytes.Contains(data, []byte("/Subtype /Type0")), toUnicode: bytes.Contains(data, []byte("/ToUnicode"))}
	var pageNumbers []int
	for _, number := range order {
		dictionary := objects[number].dictionary
		if exportedPDFPages.MatchString(dictionary) {
			if kids := exportedPDFKids.FindStringSubmatch(dictionary); kids != nil {
				for _, reference := range exportedPDFRef.FindAllStringSubmatch(kids[1], -1) {
					id := 0
					fmt.Sscanf(reference[1], "%d", &id)
					pageNumbers = append(pageNumbers, id)
				}
			}
			if count := exportedPDFCount.FindStringSubmatch(dictionary); count != nil {
				declared := 0
				fmt.Sscanf(count[1], "%d", &declared)
				if declared != len(exportedPDFRef.FindAllStringSubmatch(kidsOf(dictionary), -1)) {
					t.Fatalf("page tree /Count=%d kids mismatch", declared)
				}
			}
			if box := exportedPDFMedia.FindStringSubmatch(dictionary); box != nil {
				fmt.Sscanf(box[1], "%f", &pdf.mediaWidth)
				fmt.Sscanf(box[2], "%f", &pdf.mediaHeight)
			}
		}
	}
	if len(pageNumbers) == 0 {
		for _, number := range order {
			if exportedPDFPage.MatchString(objects[number].dictionary) && !exportedPDFPages.MatchString(objects[number].dictionary) {
				pageNumbers = append(pageNumbers, number)
			}
		}
	}
	if len(pageNumbers) == 0 {
		t.Fatal("exported PDF has no page")
	}
	if pdf.mediaWidth <= 0 {
		if box := exportedPDFMedia.FindSubmatch(data); box != nil {
			fmt.Sscanf(string(box[1]), "%f", &pdf.mediaWidth)
			fmt.Sscanf(string(box[2]), "%f", &pdf.mediaHeight)
		}
	}
	for _, number := range pageNumbers {
		dictionary := objects[number].dictionary
		var content []byte
		if contents := regexp.MustCompile(`/Contents\s+(\d+)\s+0\s+R`).FindStringSubmatch(dictionary); contents != nil {
			id := 0
			fmt.Sscanf(contents[1], "%d", &id)
			content = append(content, objects[id].decoded...)
		}
		pdf.pageTexts = append(pdf.pageTexts, exportedPDFTexts(content))
		for _, match := range exportedPDFImageOp.FindAllSubmatch(content, -1) {
			placement := exportedPDFImage{name: string(match[5])}
			fmt.Sscanf(string(match[1]), "%f", &placement.w)
			fmt.Sscanf(string(match[2]), "%f", &placement.h)
			fmt.Sscanf(string(match[3]), "%f", &placement.x)
			fmt.Sscanf(string(match[4]), "%f", &placement.y)
			pdf.images = append(pdf.images, []exportedPDFImage{placement})
		}
	}
	if file := exportedPDFFont.FindSubmatch(data); file != nil {
		id := 0
		fmt.Sscanf(string(file[1]), "%d", &id)
		pdf.fontFile = objects[id].decoded
	}
	return pdf
}

func kidsOf(dictionary string) string {
	if match := exportedPDFKids.FindStringSubmatch(dictionary); match != nil {
		return match[1]
	}
	return ""
}

// exportedPDFTexts extracts literal strings from a content stream and decodes
// the UTF-16BE bytes fpdf writes for the embedded UTF-8 font.
func exportedPDFTexts(content []byte) []string {
	var texts []string
	for index := 0; index < len(content); index++ {
		if content[index] != '(' {
			continue
		}
		var value []byte
		closed := false
		for index++; index < len(content); index++ {
			switch content[index] {
			case ')':
				closed = true
			case '\\':
				index++
				if index >= len(content) {
					break
				}
				switch escaped := content[index]; {
				case escaped == 'r':
					value = append(value, '\r')
				case escaped == 'n':
					value = append(value, '\n')
				case escaped >= '0' && escaped <= '7':
					number := int(content[index] - '0')
					for count := 0; count < 2 && index+1 < len(content) && content[index+1] >= '0' && content[index+1] <= '7'; count++ {
						index++
						number = number*8 + int(content[index]-'0')
					}
					value = append(value, byte(number))
				default:
					value = append(value, escaped)
				}
			default:
				value = append(value, content[index])
			}
			if closed {
				break
			}
		}
		if !closed {
			break
		}
		if len(value) > 0 && len(value)%2 == 0 {
			runes := make([]rune, 0, len(value)/2)
			for at := 0; at < len(value); at += 2 {
				runes = append(runes, rune(value[at])<<8|rune(value[at+1]))
			}
			texts = append(texts, string(runes))
		}
	}
	return texts
}

// TestPDFExportLongMixedDraftCoversEveryPageAndKeepsSourceIdentity exports a
// committed in-memory long Chinese draft (wide table, table image, lists,
// quote, code, unknown nodes, malicious attributes) and checks the actual PDF
// bytes in Go: page count, per-page Chinese text, section order, font
// embedding, every image placement inside the page, and an untouched source,
// draft, path and recovery journal.
func TestPDFExportLongMixedDraftCoversEveryPageAndKeepsSourceIdentity(t *testing.T) {
	requirePDFFont(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "cell.png"), imagePNG(t, 4, 2), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	var draft strings.Builder
	draft.WriteString("# 墨栈 PDF 完整长文\n\n")
	const sections = 30
	for index := 1; index <= sections; index++ {
		fmt.Fprintf(&draft, "第%02d段标记：长中文混排，含 **加粗**、*斜体*、`代码` 与 [链接](https://example.org/%d)。\n\n", index, index)
	}
	draft.WriteString("- 无序甲\n- 无序乙\n\n1. 有序甲\n2. 有序乙\n\n> 引用中文\n\n")
	draft.WriteString("| 第一列 | 第二列 | 第三列 | 第四列 | 第五列 | 第六列 |\n| --- | --- | --- | --- | --- | --- |\n")
	draft.WriteString("| ![单元格](assets/cell.png) | 右一 | 右二 | 右三 | 右四 | 右五 |\n")
	draft.WriteString("| 甲 | 乙 | 丙 | 丁 | 戊 | 己 |\n\n")
	draft.WriteString("![独立图](assets/cell.png)\n\n```go\nfmt.Println(\"代码中文\")\n```\n\n")
	draft.WriteString("<script>alert(1)</script>\n\n<x-widget onclick=\"alert(2)\">未知节点</x-widget>\n\n[危险](javascript:alert(3))\n")
	state.session.Current().Replace(draft.String())
	before := state.session.RecoverySnapshot()
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "complete.pdf")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuExportPDFID)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("export read=%v message=%q", err, state.message)
	}
	pdf := inspectExportedPDF(t, data)
	if len(pdf.pageTexts) < 2 {
		t.Fatalf("long draft produced %d page(s)", len(pdf.pageTexts))
	}
	if pdf.mediaWidth <= 0 || pdf.mediaHeight <= 0 {
		t.Fatalf("media box=%vx%v", pdf.mediaWidth, pdf.mediaHeight)
	}
	if !pdf.typeZero || !pdf.toUnicode {
		t.Fatalf("font embedding Type0=%v ToUnicode=%v", pdf.typeZero, pdf.toUnicode)
	}
	if len(pdf.fontFile) < 1024 || !bytes.HasPrefix(pdf.fontFile, []byte{0, 1, 0, 0}) {
		t.Fatalf("embedded font=%d bytes header=%x", len(pdf.fontFile), pdf.fontFile[:min(len(pdf.fontFile), 4)])
	}
	for _, forbidden := range []string{"/JavaScript", "/OpenAction", "/URI (javascript:", "/AA "} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Errorf("PDF contains active content %q", forbidden)
		}
	}
	compact := func(value string) string {
		return strings.Map(func(character rune) rune {
			if unicode.IsSpace(character) {
				return -1
			}
			return character
		}, value)
	}
	pages := make([]string, len(pdf.pageTexts))
	for index, runs := range pdf.pageTexts {
		pages[index] = compact(strings.Join(runs, ""))
		if pages[index] == "" {
			t.Errorf("page %d has no extractable text", index+1)
		}
	}
	all := compact(strings.Join(pages, ""))
	pageOfMarker := map[int]int{}
	for index, page := range pages {
		for section := 1; section <= sections; section++ {
			marker := fmt.Sprintf("第%02d段标记", section)
			if !strings.Contains(page, marker) {
				continue
			}
			if previous, ok := pageOfMarker[section]; ok {
				t.Errorf("section %d appears on pages %d and %d", section, previous+1, index+1)
			}
			pageOfMarker[section] = index
		}
	}
	previous := -1
	for section := 1; section <= sections; section++ {
		page, ok := pageOfMarker[section]
		if !ok {
			t.Errorf("section %d missing from every page", section)
			continue
		}
		if page < previous {
			t.Errorf("section order went backwards at section %d", section)
		}
		previous = page
	}
	if pageOfMarker[1] != 0 {
		t.Errorf("first section on page %d want page 1", pageOfMarker[1]+1)
	}
	if !strings.Contains(pages[len(pages)-1], compact("<script>alert(1)</script>")) {
		t.Errorf("last page lost inert trailing content")
	}
	for _, expected := range []string{
		"墨栈 PDF 完整长文", "加粗", "斜体", "代码", "链接",
		"无序甲", "无序乙", "有序甲", "有序乙", "引用中文",
		"第一列", "第二列", "第三列", "第四列", "第五列", "第六列",
		"右一", "右五", "甲", "乙", "丙", "丁", "戊", "己",
		"fmt.Println", "代码中文", "未知节点", "<script>alert(1)</script>",
	} {
		if !strings.Contains(all, compact(expected)) {
			t.Errorf("PDF text lost %q", expected)
		}
	}
	placements := 0
	for pageIndex, images := range pdf.images {
		for _, image := range images {
			placements++
			if image.w <= 0 || image.h <= 0 {
				t.Errorf("page %d image %q empty rect", pageIndex+1, image.name)
			}
			if image.x < 0 || image.y < 0 || image.x+image.w > pdf.mediaWidth+0.01 || image.y+image.h > pdf.mediaHeight+0.01 {
				t.Errorf("page %d image %q cropped: %+v media=%vx%v", pageIndex+1, image.name, image, pdf.mediaWidth, pdf.mediaHeight)
			}
		}
	}
	if placements != 2 {
		t.Errorf("image placements=%d want table cell and standalone image", placements)
	}
	current := state.session.Current()
	if current.Path != path || !current.Dirty() || current.Text() != draft.String() {
		t.Fatalf("PDF export changed tab identity: path=%q dirty=%v", current.Path, current.Dirty())
	}
	if after := state.session.RecoverySnapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("PDF export changed the recovery journal snapshot")
	}
	if original, err := os.ReadFile(path); err != nil || string(original) != "# Original\n" {
		t.Fatalf("PDF export changed source bytes: %q, %v", original, err)
	}
}
