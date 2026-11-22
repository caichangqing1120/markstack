package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoveryTextBudgetMatchesJSONEscapes(t *testing.T) {
	check := func(drafts []RecoveryDraft) {
		t.Helper()
		encodedBytes := 0
		var wire bytes.Buffer
		encoder := json.NewEncoder(&wire)
		encoder.SetEscapeHTML(false)
		for _, draft := range drafts {
			for _, value := range [...]string{draft.Body, draft.Saved} {
				wire.Reset()
				if err := encoder.Encode(value); err != nil {
					t.Fatal(err)
				}
				encodedBytes += wire.Len() - len("\"\"\n")
			}
		}
		for limit := 0; limit <= 6*encodedBytes+1; limit++ {
			if got, want := recoveryTextExceedsWireLimit(drafts, limit), encodedBytes > limit; got != want {
				t.Fatalf("drafts=%#v: %d encoded bytes at limit %d: exceeds=%t, want %t", drafts, encodedBytes, limit, got, want)
			}
		}
	}
	check(nil)
	for symbol := rune(1); symbol < 128; symbol++ {
		check([]RecoveryDraft{{Body: string(symbol), Saved: "a"}})
	}
	check([]RecoveryDraft{{Body: "\u2028\u2029<>&", Saved: "\u4f60\u597d"}, {Body: "\x01\x02\u0301", Saved: "\U0001f603\r\n\"\\"}})
}

func TestRecoveryStorePreservesV1WireBoundary(t *testing.T) {
	store := RecoveryStore{Path: filepath.Join(t.TempDir(), "state", "recovery.json")}
	prefix := "\x01\"\\\t\n\r\b\f\u2028\u2029\u4f60<>&"
	full := prefix + strings.Repeat("b", maxDraftBytes-len(prefix))
	snapshot := RecoverySnapshot{Version: recoveryVersion, ActiveID: 2, Drafts: []RecoveryDraft{
		{ID: 1, Title: "first", Body: full, Saved: "X" + full[1:], Newline: "\n"},
		{ID: 2, Title: "second", Body: full, Saved: "Y" + full[1:], Newline: "\n"},
	}}
	encode := func() []byte {
		t.Helper()
		var wire bytes.Buffer
		encoder := json.NewEncoder(&wire)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(snapshot); err != nil {
			t.Fatal(err)
		}
		return bytes.TrimSuffix(wire.Bytes(), []byte("\n"))
	}
	extra := len(encode()) - maxRecoveryBytes
	if extra <= 0 || extra >= maxDraftBytes-len(prefix) {
		t.Fatalf("unexpected wire overhead: %d bytes", extra)
	}
	snapshot.Drafts[1].Saved = snapshot.Drafts[1].Saved[:maxDraftBytes-extra]
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("exact limit: %v", err)
	}
	before, err := os.ReadFile(store.Path)
	if err != nil || len(before) != maxRecoveryBytes || !bytes.Equal(before, encode()) {
		t.Fatalf("exact-limit v1 bytes changed: %v", err)
	}
	snapshot.Drafts[1].Saved += "b"
	if err := store.Save(snapshot); !errors.Is(err, ErrCorruptRecovery) || !strings.Contains(err.Error(), "journal exceeds size limit") {
		t.Fatalf("limit plus one: %v", err)
	}
	if after, err := os.ReadFile(store.Path); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("previous journal changed after refusal: %v", err)
	}
}
