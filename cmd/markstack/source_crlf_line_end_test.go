package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSourceLineEndKeepsCRLFPairIntact(test *testing.T) {
	for _, kind := range []struct {
		name, extension string
	}{
		{"code", ".go"},
		{"markdown", ".md"},
	} {
		test.Run(kind.name, func(context *testing.T) {
			directory := context.TempDir()
			for _, name := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
				context.Setenv("MARKSTACK_"+name+"_PATH", filepath.Join(directory, name+".json"))
			}
			path := filepath.Join(directory, "sample"+kind.extension)
			original := []byte("\ufeff// alpha1\r\n// alpha2\r\n")
			if err := os.WriteFile(path, original, 0640); err != nil {
				context.Fatal(err)
			}
			state, driver := testDriver(context)
			if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
				context.Fatal(err)
			}
			clickID(context, driver, openFileID)
			if kind.extension == ".md" {
				clickID(context, driver, editModeID)
			}
			clickID(context, driver, sourceID)
			if err := driver.SelectText(0, 0); err != nil {
				context.Fatal(err)
			}
			if err := driver.Key(gfx.KeyEnd, 0); err != nil {
				context.Fatal(err)
			}
			if err := driver.Type("X"); err != nil {
				context.Fatal(err)
			}
			want := "// alpha1X\r\n// alpha2\r\n"
			if got := state.session.Current().Text(); got != want {
				context.Fatalf("End inserted across CRLF: got %q, want %q", got, want)
			}
			clickID(context, driver, saveID)
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, []byte("\ufeff"+want)) {
				context.Fatalf("saved bytes = %q, err=%v", data, err)
			}
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0640 {
				context.Fatal("source mode changed")
			}
		})
	}
}
