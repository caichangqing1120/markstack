package richedit

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type tokenCancelContext struct {
	context.Context
	cancel   context.CancelFunc
	checks   int
	cancelAt int
}

func (ctx *tokenCancelContext) Err() error {
	ctx.checks++
	if ctx.checks == ctx.cancelAt {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestParseContextStopsWithoutReturningPartialDocument(t *testing.T) {
	source := []byte("<p>first complete block</p><p>" + strings.Repeat("<strong>next</strong>", 128) + "</p>")
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &tokenCancelContext{Context: parent, cancel: cancel, cancelAt: 16}
	document, err := ParseContext(ctx, source)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(document, Document{}) {
		t.Fatalf("expected cancellation without partial document, got %+v and %v", document, err)
	}
}

func TestParseContextStopsInsideSingleLongToken(t *testing.T) {
	const prefix = "<p>first complete block</p>"
	for _, fixture := range []struct{ name, opening, closing string }{
		{"comment", "<!--", "-->"},
		{"text", "<p>", "</p>"},
		{"attribute", `<p title="`, `">tail</p>`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := []byte(prefix + fixture.opening + strings.Repeat("x", 1<<20) + fixture.closing)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &tokenCancelContext{Context: parent, cancel: cancel, cancelAt: 12}
			document, err := ParseContext(ctx, source)
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(document, Document{}) {
				t.Fatalf("long token returned %d blocks after %d context checks, err=%v", len(document.Blocks), ctx.checks, err)
			}
		})
	}
}

func TestParseContextCancelableReaderKeepsCompleteProjection(t *testing.T) {
	for _, source := range [][]byte{
		[]byte("\xef\xbb\xbf<p>" + strings.Repeat("\r\n\u4e2d &amp; text", 32<<10) + "</p>"),
		[]byte("<p>first</p><!--" + strings.Repeat("comment ", 32<<10) + "--><p>tail</p>"),
		[]byte(`<p>visible<a href="` + strings.Repeat("truncated", 32<<10)),
	} {
		ctx, cancel := context.WithCancel(context.Background())
		document, err := ParseContext(ctx, source)
		cancel()
		if err != nil || !reflect.DeepEqual(document, Parse(source)) {
			t.Fatalf("cancelable reader changed the completed projection of a %d-byte fixture: %v", len(source), err)
		}
	}
}

func TestParseContextCancelsDuringUnclosedContainerFinalization(t *testing.T) {
	const containers = 16
	const paragraphs = 16
	source := []byte(strings.Repeat("<ul>", containers) + strings.Repeat("<p>needle</p>", paragraphs))
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	const tokenChecks = 1 + containers + 3*paragraphs + 1
	const tokenizerReads = 2
	const checksWithoutNestedTraversal = tokenChecks + tokenizerReads + 2*containers + 2*paragraphs
	ctx := &tokenCancelContext{Context: parent, cancel: cancel, cancelAt: checksWithoutNestedTraversal + 1}
	document, err := ParseContext(ctx, source)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(document, Document{}) {
		t.Fatalf("finalization returned %d blocks after %d checks, err=%v", len(document.Blocks), ctx.checks, err)
	}
}

func TestParseContextKeepsCompleteProjection(t *testing.T) {
	for _, fixture := range []string{
		"\xef\xbb\xbf<p><strong>a &amp; b</strong><br>tail</p>\r\n<unknown>literal</unknown>",
		"<p>before<strong>middle</strong><a href=\"unterminated",
		"<p>\xff</p>",
		"<ul><p>closed</p><p>open",
	} {
		source := []byte(fixture)
		document, err := ParseContext(context.Background(), source)
		if err != nil || !reflect.DeepEqual(document, Parse(source)) {
			t.Fatalf("complete parse changed for %q: %v", fixture, err)
		}
	}
}
