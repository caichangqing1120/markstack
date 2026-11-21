//go:build darwin

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"markstack/internal/session"
)

func TestRecoveryStartupWarningSurvivesRefreshAndSave(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "markstack-headless")
	if output, err := exec.Command("go", "build", "-tags", "vivahead", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build headless app: %v\n%s", err, output)
	}
	for _, fixture := range []struct {
		name, content string
		mode          os.FileMode
		link          bool
		cause         error
	}{
		{"malformed", "{invalid", 0600, false, session.ErrCorruptRecovery},
		{"future-version", `{"version":2,"drafts":[]}`, 0600, false, session.ErrCorruptRecovery},
		{"loose-permissions", `{"version":1,"drafts":[]}`, 0644, false, session.ErrUnsafeRecoveryPath},
		{"symlink", `{"version":1,"drafts":[]}`, 0600, true, session.ErrUnsafeRecoveryPath},
		{"healthy", "", 0600, false, nil},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			private := filepath.Join(root, "state")
			for _, directory := range []string{workspace, private} {
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			document := filepath.Join(workspace, "note.md")
			writePrivate := func(path, content string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			writePrivate(document, "\ufefforiginal\r\n", 0640)
			journal := filepath.Join(private, "recovery.json")
			var before os.FileInfo
			check := `expect no-text "恢复快照不可用"`
			if fixture.cause != nil {
				check = `expect text "恢复快照不可用：` + fixture.cause.Error() + `"`
				if fixture.link {
					writePrivate(filepath.Join(private, "sentinel.json"), fixture.content, fixture.mode)
					if err := os.Symlink("sentinel.json", journal); err != nil {
						t.Fatal(err)
					}
				} else {
					writePrivate(journal, fixture.content, fixture.mode)
				}
				info, err := os.Lstat(journal)
				if err != nil {
					t.Fatal(err)
				}
				before = info
			}
			commands := []string{
				"resize 740 720", check,
				`click "刷新工作区"`, `expect text "文件树已刷新"`, check,
				`click "note.md"`, `click "Markdown 源码编辑器"`, "key cmd+a", "type updated", "key cmd+s",
				`expect text "已保存 note.md"`, check, "quit", "",
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, workspace)
			command.Env = []string{
				"HOME=" + root, "PATH=" + os.Getenv("PATH"), "VIVA_HEADLESS=1", "VIVA_HEADLESS_SCALE=1",
				"MARKSTACK_SETTINGS_PATH=" + filepath.Join(private, "settings.json"),
				"MARKSTACK_RECOVERY_PATH=" + journal,
				"MARKSTACK_SESSION_PATH=" + filepath.Join(private, "session.json"),
				"MARKSTACK_RECENT_PATH=" + filepath.Join(private, "recent.json"),
			}
			command.Stdin = strings.NewReader(strings.Join(commands, "\n"))
			output, err := command.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "ok ready") {
				t.Fatalf("headless replay: %v\n%s", err, output)
			}
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "error ") {
					t.Errorf("headless assertion: %s", line)
				}
			}
			data, err := os.ReadFile(document)
			if err != nil || string(data) != "\ufeffupdated" {
				t.Fatalf("explicit save = %q, %v", data, err)
			}
			if info, err := os.Stat(document); err != nil || info.Mode().Perm() != 0640 {
				t.Fatalf("explicit save changed document permissions: %v", err)
			}
			after, err := os.Lstat(journal)
			if before == nil {
				if !os.IsNotExist(err) {
					t.Fatalf("clean exit left a recovery journal: %v", err)
				}
				return
			}
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("rejected journal identity or permissions changed: %v", err)
			}
			if data, err := os.ReadFile(journal); err != nil || string(data) != fixture.content {
				t.Fatalf("rejected journal or link target changed: %q, %v", data, err)
			}
		})
	}
}
