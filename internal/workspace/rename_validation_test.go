package workspace

import "testing"

func TestInvalidWindowsFileName(t *testing.T) {
	for _, bad := range []string{
		"", ".", "..",
		"NUL", "NUL.md", "con.txt", "prn", "aux.md", "COM1", "com9.md", "LPT1.md",
		"C:relative.md", "attachment.md:stream", "trailing.md.", "trailing ",
		"bad<name>.md", "bad?name.md", "bad*name.md", "bad|name.md",
	} {
		if !invalidWindowsFileName(bad) {
			t.Errorf("expected %q to be rejected on Windows", bad)
		}
	}
	for _, good := range []string{"note.md", "流程图.md", "CON1.md", "null.md", "my.note.md"} {
		if invalidWindowsFileName(good) {
			t.Errorf("expected %q to be accepted on Windows", good)
		}
	}
}
