package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestRenderedFormattingKeepsPendingComposition(test *testing.T) {
	type formatCommand struct {
		name     string
		id       gfx.ID
		key      rune
		callback func(*model)
	}
	commands := []formatCommand{
		{name: "bold-button", id: boldBlockID},
		{name: "italic-button", id: italicBlockID},
		{name: "strike-button", id: strikeBlockID},
		{name: "bold-shortcut", key: 'b'},
		{name: "italic-shortcut", key: 'i'},
		{name: "link-button", id: linkBlockID},
		{name: "link-shortcut", key: 'k'},
		{name: "heading-menu", id: headingMenuID},
	}
	for level := 0; level <= 6; level++ {
		commands = append(commands, formatCommand{
			name:     fmt.Sprintf("heading-callback-%d", level),
			callback: func(state *model) { state.applyHeadingLevel(level) },
		})
	}
	for option := 0; option <= 5; option++ {
		commands = append(commands, formatCommand{
			name:     fmt.Sprintf("structure-callback-%d", option),
			callback: func(state *model) { state.applyBlockFormat(option) },
		})
	}
	for _, command := range commands {
		for _, dirty := range []bool{false, true} {
			for _, preedit := range []string{"ni", ""} {
				test.Run(fmt.Sprintf("%s/dirty-%v/preedit-%q", command.name, dirty, preedit), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, "# Title\n\nabc\n\nTail\n", false, 1200)
					clickID(context, driver, renderedRows.At(1))
					if dirty {
						if err := driver.Type("Q"); err != nil {
							context.Fatal(err)
						}
					}
					if err := driver.SelectText(1, 2); err != nil {
						context.Fatal(err)
					}
					source := state.session.Current().Text()
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					if preedit == "" {
						if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextSetMarked, Text: ""}); err != nil {
							context.Fatal(err)
						}
						state.invalidator.Invalidate()
						if err := driver.Pump(); err != nil {
							context.Fatal(err)
						}
					}
					if state.pendingCompositionFocus() != richInputID || state.session.Current().Text() != source || state.session.Current().Dirty() != dirty {
						context.Fatal("fixture did not retain an uncommitted composition")
					}
					owner := state.rich.input
					before, selection := owner.String(), owner.Selection()
					nativeBefore := app.TextStateOf(owner)
					if command.key != 0 {
						if _, err := driver.ShortcutChord(primaryModifier(), command.key); err != nil {
							context.Fatal(err)
						}
					} else if command.callback != nil {
						command.callback(state)
					} else {
						pressID(context, driver, command.id)
						requireNativeComposition(context, driver, richInputID, nativeBefore, "format press")
						releaseID(context, driver, command.id)
						requireNativeComposition(context, driver, richInputID, nativeBefore, "format release")
					}
					if state.session.Current().Text() != source || state.session.Current().Dirty() != dirty {
						context.Errorf("format changed committed source during composition: %q", state.session.Current().Text())
					}
					if state.rich.input != owner || owner.String() != before || owner.Selection() != selection || state.pendingCompositionFocus() != richInputID {
						context.Fatalf("format discarded or moved the pending composition: source=%q input-replaced=%v pending-focus=%v original-input=%q", state.session.Current().Text(), state.rich.input != owner, state.pendingCompositionFocus(), owner.String())
					}
					if driver.Snapshot().Focused != richInputID {
						pumpQueuedFrame(context, driver, "blocked format")
					}
					if driver.Snapshot().Focused != richInputID || state.linkOpen {
						context.Fatal("blocked format did not retain the composition owner focus")
					}
					requireNativeComposition(context, driver, richInputID, nativeBefore, "blocked format")
					if err := driver.Compose("nihao"); err != nil {
						context.Fatal(err)
					}
					continued := app.TextStateOf(owner)
					requireCompositionExtended(context, "blocked format continuation", nativeBefore, continued)
					requireNativeComposition(context, driver, richInputID, continued, "blocked format continuation")
					if state.session.Current().Text() != source || state.session.Current().Dirty() != dirty {
						context.Fatal("continued composition changed the committed draft")
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
						context.Fatal(err)
					}
					if saved, err := os.ReadFile(filename); err != nil || !bytes.Equal(saved, original) {
						context.Errorf("format allowed a composing document to save changed bytes: %q, %v", saved, err)
					}
					if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
						context.Fatal(err)
					}
					state.invalidator.Invalidate()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					if state.session.Current().Text() != source || state.session.Current().Dirty() != dirty {
						context.Fatal("cancellation did not restore the original committed draft")
					}
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(source, "abc", "aXc", 1))...)
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					baseline := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
					expectRenderedCollisionSave(context, state, driver, filename, baseline)
					if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, baseline)
					if dirty {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedLinkFormatKeepsURLComposition(test *testing.T) {
	for _, target := range []struct {
		name string
		id   gfx.ID
	}{
		{name: "apply-link", id: linkApplyID},
		{name: "bold-button", id: boldBlockID},
	} {
		test.Run(target.name, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "# Title\n\nabc\n\nTail\n", false, 1200)
			clickID(context, driver, renderedRows.At(1))
			clickID(context, driver, linkBlockID)
			if err := driver.Type("https://example."); err != nil {
				context.Fatal(err)
			}
			if err := driver.Compose("invalid"); err != nil {
				context.Fatal(err)
			}
			source := state.session.Current().Text()
			owner := state.linkEditor
			before, selection := owner.String(), owner.Selection()
			nativeBefore := app.TextStateOf(owner)
			pressID(context, driver, target.id)
			requireNativeComposition(context, driver, linkInputID, nativeBefore, "URL format press")
			releaseID(context, driver, target.id)
			requireNativeComposition(context, driver, linkInputID, nativeBefore, "URL format release")
			if state.session.Current().Text() != source || state.session.Current().Dirty() {
				context.Errorf("format committed a pending URL composition: %q", state.session.Current().Text())
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				context.Fatal(err)
			}
			if saved, err := os.ReadFile(filename); err != nil || !bytes.Equal(saved, original) {
				context.Errorf("pending URL composition reached disk: %q, %v", saved, err)
			}
			if !state.linkOpen || state.linkEditor != owner || owner.String() != before || owner.Selection() != selection {
				context.Fatal("format discarded the pending link editor")
			}
			if _, composing := owner.Composition(); !composing {
				context.Fatal("format ended the URL composition")
			}
			if driver.Snapshot().Focused != linkInputID {
				pumpQueuedFrame(context, driver, "blocked URL format")
			}
			if driver.Snapshot().Focused != linkInputID {
				context.Fatal("format did not return focus to the composing URL")
			}
			if err := driver.Compose("invalidx"); err != nil {
				context.Fatal(err)
			}
			continued := app.TextStateOf(owner)
			requireCompositionExtended(context, "blocked URL format continuation", nativeBefore, continued)
			requireNativeComposition(context, driver, linkInputID, continued, "blocked URL format continuation")
			owner.CancelComposition()
			state.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				context.Fatal(err)
			}
			if err := driver.Type("org"); err != nil {
				context.Fatal(err)
			}
			clickID(context, driver, linkApplyID)
			wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(source, "abc", "[abc](https://example.org)", 1))...)
			expectRenderedCollisionSave(context, state, driver, filename, wanted)
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}
