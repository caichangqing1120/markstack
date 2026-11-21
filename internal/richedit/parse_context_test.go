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

func TestParseContextCancelsDuringUnclosedContainerFinalization(t *testing.T) {
	const containers = 16
	const paragraphs = 16
	source := []byte(strings.Repeat("<ul>", containers) + strings.Repeat("<p>needle</p>", paragraphs))
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	const tokenChecks = 1 + containers + 3*paragraphs + 1
	const checksWithoutNestedTraversal = tokenChecks + 2*containers + 2*paragraphs
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
