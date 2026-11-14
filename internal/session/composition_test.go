package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/text"
)

func TestUncommittedCompositionCannotDirtySaveOrQuitSilently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("a中b\r\n")...)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state := New()
	document, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Editor.SetSelection(text.Range{Start: 1, End: 2})
	document.Editor.SetMarkedText("pin", text.Range{Start: 3, End: 3})
	if document.Text() != "apinb\r\n" || document.Dirty() {
		t.Fatalf("preedit text=%q dirty=%v", document.Text(), document.Dirty())
	}
	if err := state.Save(state.Active, ""); !errors.Is(err, ErrCompositionPending) {
		t.Fatalf("save pending preedit=%v", err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("preedit changed disk: %q, %v", data, err)
	}
	if state.RequestQuit() || state.Pending() == nil || state.Pending().Document != document {
		t.Fatal("quit discarded an active composition without confirmation")
	}
	if completed, err := state.Resolve(Save, ""); completed || !errors.Is(err, ErrCompositionPending) || state.Pending() == nil {
		t.Fatalf("quit Save preedit completed=%v err=%v pending=%v", completed, err, state.Pending())
	}
	if _, err := state.Resolve(Cancel, ""); err != nil {
		t.Fatal(err)
	}
	document.Editor.Insert("新")
	if document.Text() != "a新b\r\n" || !document.Dirty() {
		t.Fatalf("committed text=%q dirty=%v", document.Text(), document.Dirty())
	}
	if err := state.Save(state.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, append([]byte{0xef, 0xbb, 0xbf}, []byte("a新b\r\n")...)) {
		t.Fatalf("committed save=%q, %v", data, err)
	}
}

func TestRecoveryJournalKeepsCommittedTextButNotActivePreedit(t *testing.T) {
	state := New()
	document := state.Current()
	document.Editor.Insert("已提交")
	document.Editor.SetMarkedText("pin", text.Range{Start: 3, End: 3})
	if document.Text() != "已提交pin" || !document.Dirty() {
		t.Fatalf("composing dirty draft=%q dirty=%v", document.Text(), document.Dirty())
	}
	snapshot := state.RecoverySnapshot()
	if len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Body != "已提交" {
		t.Fatalf("recovery stored uncommitted preedit: %+v", snapshot.Drafts)
	}
	if err := state.Save(state.Active, filepath.Join(t.TempDir(), "draft.md")); !errors.Is(err, ErrCompositionPending) {
		t.Fatalf("Save As pending preedit=%v", err)
	}
}
