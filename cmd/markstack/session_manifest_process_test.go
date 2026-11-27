//go:build darwin

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"markstack/internal/session"
)

func TestSessionManifestReopensCleanTabsAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	workspace = resolved
	first, second := filepath.Join(workspace, "first.md"), filepath.Join(workspace, "second.md")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("untouched"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := session.SessionStore{Path: filepath.Join(private, "session.json")}
	recovery := session.RecoveryStore{Path: filepath.Join(private, "recovery.json")}
	settingsPath := filepath.Join(private, "settings.json")
	prefs := session.DefaultPreferences()
	prefs.Language = "zh-CN"
	if err := (session.PreferencesStore{Path: settingsPath}).Save(prefs); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "markstack-headless")
	if output, err := exec.Command("go", "build", "-tags", "vivahead", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build headless app: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	launch := func(arguments ...string) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
		command := exec.CommandContext(ctx, binary, arguments...)
		command.Env = []string{
			"HOME=" + root, "PATH=" + os.Getenv("PATH"), "VIVA_HEADLESS=1",
			"MARKSTACK_SETTINGS_PATH=" + settingsPath,
			"MARKSTACK_SESSION_PATH=" + manifest.Path, "MARKSTACK_RECOVERY_PATH=" + recovery.Path,
		}
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(output)
		if line, err := reader.ReadString('\n'); err != nil || !strings.Contains(line, "ok ready") {
			t.Fatalf("startup=%q err=%v", line, err)
		}
		return command, input, reader
	}
	send := func(input io.Writer, reader *bufio.Reader, command string) string {
		t.Helper()
		if _, err := fmt.Fprintln(input, command); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "ok") {
			t.Fatalf("%q: %q, %v", command, line, err)
		}
		return line
	}
	firstRun, input, reader := launch(workspace)
	defer firstRun.Process.Kill()
	send(input, reader, `click "first.md"`)
	send(input, reader, `click "second.md"`)
	send(input, reader, `click "Markdown 源码编辑器"`)
	send(input, reader, "key home")
	send(input, reader, "key right")
	send(input, reader, "quit")
	input.Close()
	if err := firstRun.Wait(); err != nil {
		t.Fatal(err)
	}
	saved, err := manifest.Load()
	if err != nil || len(saved.Tabs) != 2 || saved.ActiveID != saved.Tabs[1].ID || saved.Tabs[0].Path != first || saved.Tabs[1].Path != second || saved.Tabs[1].Caret != 1 {
		t.Fatalf("first process manifest=%+v, %v", saved, err)
	}
	secondRun, input, reader := launch()
	defer secondRun.Process.Kill()
	if line := send(input, reader, "editor"); !strings.Contains(line, `value="untouched"`) || !strings.Contains(line, "selection=1,1") {
		t.Fatalf("restored active editor=%q", line)
	}
	send(input, reader, "type 新")
	if line := send(input, reader, "editor"); !strings.Contains(line, `value="u新ntouched"`) {
		t.Fatalf("typing after restore missed the caret: %q", line)
	}
	send(input, reader, "key cmd+z")
	send(input, reader, "key cmd+s")
	send(input, reader, "quit")
	input.Close()
	if err := secondRun.Wait(); err != nil {
		t.Fatal(err)
	}
	thirdRun, input, reader := launch()
	defer thirdRun.Process.Kill()
	send(input, reader, `click "first.md"`)
	send(input, reader, `click "Markdown 源码编辑器"`)
	send(input, reader, "key home")
	send(input, reader, "key right")
	send(input, reader, "key right")
	deadline := time.After(5 * time.Second)
	for {
		latest, err := manifest.Load()
		if err == nil && len(latest.Tabs) == 2 && latest.ActiveID == latest.Tabs[0].ID && latest.Tabs[0].Caret == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("latest caret did not reach manifest: %+v, %v", latest, err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := thirdRun.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = thirdRun.Wait()
	input.Close()
	fourthRun, input, reader := launch()
	defer fourthRun.Process.Kill()
	if line := send(input, reader, "editor"); !strings.Contains(line, `value="untouched"`) || !strings.Contains(line, "selection=2,2") {
		t.Fatalf("crash-restored active caret=%q", line)
	}
	send(input, reader, "quit")
	input.Close()
	if err := fourthRun.Wait(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "untouched" {
			t.Fatalf("session restore wrote %s: %q, %v", path, data, err)
		}
	}
	if _, err := os.Lstat(recovery.Path); !os.IsNotExist(err) {
		t.Fatalf("clean tabs created a dirty recovery journal: %v", err)
	}
}

func TestExplicitWorkspaceCancelKeepsRecoveredCleanAndDirtyTabs(t *testing.T) {
	root := t.TempDir()
	oldRoot, newRoot := filepath.Join(root, "old"), filepath.Join(root, "new")
	for _, path := range []string{oldRoot, newRoot} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	clean, dirty := filepath.Join(oldRoot, "clean.md"), filepath.Join(oldRoot, "dirty.md")
	for _, path := range []string{clean, dirty} {
		if err := os.WriteFile(path, []byte("disk"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opened := session.New()
	opened.Root = oldRoot
	if _, err := opened.Open(clean); err != nil {
		t.Fatal(err)
	}
	draft, err := opened.Open(dirty)
	if err != nil {
		t.Fatal(err)
	}
	draft.Replace("local draft")
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := session.SessionStore{Path: filepath.Join(private, "session.json")}
	recovery := session.RecoveryStore{Path: filepath.Join(private, "recovery.json")}
	if err := manifest.Save(opened.Manifest()); err != nil {
		t.Fatal(err)
	}
	if err := recovery.Save(opened.RecoverySnapshot()); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "markstack-headless")
	if output, err := exec.Command("go", "build", "-tags", "vivahead", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build headless app: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	settingsPath := filepath.Join(private, "settings.json")
	prefs := session.DefaultPreferences()
	prefs.Language = "zh-CN"
	if err := (session.PreferencesStore{Path: settingsPath}).Save(prefs); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, newRoot)
	command.Env = []string{
		"HOME=" + root, "PATH=" + os.Getenv("PATH"), "VIVA_HEADLESS=1",
		"MARKSTACK_SETTINGS_PATH=" + settingsPath,
		"MARKSTACK_SESSION_PATH=" + manifest.Path, "MARKSTACK_RECOVERY_PATH=" + recovery.Path,
	}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		input.Close()
		command.Process.Kill()
		_ = command.Wait()
	}()
	reader := bufio.NewReader(output)
	if line, err := reader.ReadString('\n'); err != nil || !strings.Contains(line, "ok ready") {
		t.Fatalf("startup=%q, %v", line, err)
	}
	for _, step := range []string{`click "取消"`, `find "关闭 clean.md"`, `find "关闭 dirty.md"`} {
		if _, err := fmt.Fprintln(input, step); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "ok") {
			t.Fatalf("%q: %q, %v", step, line, err)
		}
	}
	if data, err := os.ReadFile(dirty); err != nil || string(data) != "disk" {
		t.Fatalf("canceled workspace rewrote dirty source: %q, %v", data, err)
	}
}

func TestKilledProcessRestoresCleanAndDirtyFourDocumentKinds(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	workspace = resolved
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := session.SessionStore{Path: filepath.Join(private, "session.json")}
	recovery := session.RecoveryStore{Path: filepath.Join(private, "recovery.json")}
	originals := map[string][]byte{
		"a.md":  append([]byte{0xef, 0xbb, 0xbf}, []byte("# 标题\r\n正文\r\n")...),
		"e.md":  []byte("clean body\n"),
		"b.mh":  []byte("<p>甲</p>\r\n"),
		"c.go":  []byte("package main\n\nfunc main() {}\n"),
		"d.txt": []byte("纯文本\n"),
	}
	for name, body := range originals {
		if err := os.WriteFile(filepath.Join(workspace, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(root, "markstack-headless")
	if output, err := exec.Command("go", "build", "-tags", "vivahead", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build headless app: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	settingsPath := filepath.Join(private, "settings.json")
	prefs := session.DefaultPreferences()
	prefs.Language = "zh-CN"
	if err := (session.PreferencesStore{Path: settingsPath}).Save(prefs); err != nil {
		t.Fatal(err)
	}
	launch := func(arguments ...string) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
		command := exec.CommandContext(ctx, binary, arguments...)
		command.Env = []string{
			"HOME=" + root, "PATH=" + os.Getenv("PATH"), "VIVA_HEADLESS=1",
			"MARKSTACK_SETTINGS_PATH=" + settingsPath,
			"MARKSTACK_SESSION_PATH=" + manifest.Path, "MARKSTACK_RECOVERY_PATH=" + recovery.Path,
		}
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(output)
		if line, err := reader.ReadString('\n'); err != nil || !strings.Contains(line, "ok ready") {
			t.Fatalf("startup=%q err=%v", line, err)
		}
		return command, input, reader
	}
	send := func(input io.Writer, reader *bufio.Reader, command string) string {
		t.Helper()
		if _, err := fmt.Fprintln(input, command); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "ok") {
			t.Fatalf("%q: %q, %v", command, line, err)
		}
		return line
	}
	waitFor := func(description string, condition func() bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			if condition() {
				return
			}
			select {
			case <-deadline:
				t.Fatalf("%s did not settle", description)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	first, input, reader := launch(workspace)
	defer first.Process.Kill()
	for _, step := range []string{
		`click "a.md"`, `click "Markdown 源码编辑器"`, "select 0 0", "type A",
		`click "e.md"`, `click "Markdown 源码编辑器"`, "select 1 1",
		`click "b.mh"`, `click "源码"`, `click "HTML 原文编辑器"`, "select 0 0", "type B",
		`click "c.go"`, `click "代码编辑器"`, "select 0 0", "type C",
		`click "d.txt"`, `click "纯文本编辑器"`, "select 0 0", "type D",
	} {
		send(input, reader, step)
	}
	dirtyBodies := []string{"A# 标题\r\n正文\r\n", "B<p>甲</p>\r\n", "Cpackage main\n\nfunc main() {}\n", "D纯文本\n"}
	dirtyNames := []string{"a.md", "b.mh", "c.go", "d.txt"}
	journalSettled := func() bool {
		snapshot, err := recovery.Load()
		if err != nil || len(snapshot.Drafts) != len(dirtyNames) {
			return false
		}
		for index, draft := range snapshot.Drafts {
			if draft.Title != dirtyNames[index] || draft.Body != dirtyBodies[index] || !draft.DiskExists || draft.DiskSHA256 != sha256.Sum256(originals[dirtyNames[index]]) {
				return false
			}
		}
		return snapshot.Drafts[0].BOM && snapshot.Drafts[0].Newline == "\r\n"
	}
	journalDeadline := time.After(5 * time.Second)
	for !journalSettled() {
		select {
		case <-journalDeadline:
			journal, journalErr := recovery.Load()
			t.Fatalf("journal drafts: %s (err %v)", describeRecoveryDrafts(journal), journalErr)
		case <-time.After(10 * time.Millisecond):
		}
	}
	waitFor("session manifest", func() bool {
		saved, err := manifest.Load()
		if err != nil || len(saved.Tabs) != 5 || saved.Root != workspace {
			return false
		}
		names := []string{"a.md", "e.md", "b.mh", "c.go", "d.txt"}
		dirty := []bool{true, false, true, true, true}
		for index, tab := range saved.Tabs {
			if tab.Path != filepath.Join(workspace, names[index]) || tab.Dirty != dirty[index] || tab.Caret != 1 {
				return false
			}
		}
		return saved.ActiveID == saved.Tabs[4].ID
	})
	for name, body := range originals {
		if data, err := os.ReadFile(filepath.Join(workspace, name)); err != nil || !bytes.Equal(data, body) {
			t.Fatalf("committed input or journal wrote %s: %q, %v", name, data, err)
		}
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	input.Close()
	second, input, reader := launch()
	defer func() {
		input.Close()
		second.Process.Kill()
		_ = second.Wait()
	}()
	line := send(input, reader, "editor")
	if !strings.Contains(line, fmt.Sprintf("value=%q selection=1,1", dirtyBodies[3])) {
		t.Fatalf("crash-restored active plain editor=%q", line)
	}
	for _, check := range []struct{ tab, editor, value string }{
		{"tab:a.md", "Markdown 源码编辑器", dirtyBodies[0]},
		{"tab:e.md", "Markdown 源码编辑器", string(originals["e.md"])},
		{"tab:b.mh", "HTML 原文编辑器", dirtyBodies[1]},
		{"tab:c.go", "代码编辑器", dirtyBodies[2]},
		{"tab:d.txt", "纯文本编辑器", dirtyBodies[3]},
	} {
		send(input, reader, fmt.Sprintf("click %q", check.tab))
		send(input, reader, `click "源码"`)
		send(input, reader, "settle 50")
		send(input, reader, fmt.Sprintf("find %q", check.editor))
		if line := send(input, reader, "editor"); !strings.Contains(line, fmt.Sprintf("value=%q selection=1,1", check.value)) {
			t.Fatalf("%s restored editor=%q, want value=%q", check.tab, line, check.value)
		}
	}
	for name, body := range originals {
		if data, err := os.ReadFile(filepath.Join(workspace, name)); err != nil || !bytes.Equal(data, body) {
			t.Fatalf("recovery overwrote %s: %q, %v", name, data, err)
		}
	}
	if snapshot, err := recovery.Load(); err != nil || len(snapshot.Drafts) != len(dirtyNames) {
		t.Fatalf("startup erased dirty recovery: %+v, %v", snapshot.Drafts, err)
	}
	if saved, err := manifest.Load(); err != nil || len(saved.Tabs) != 5 || saved.ActiveID != saved.Tabs[4].ID {
		t.Fatalf("startup rewrote manifest: %+v, %v", saved, err)
	}
}

func describeRecoveryDrafts(snapshot session.RecoverySnapshot) string {
	var parts []string
	for _, draft := range snapshot.Drafts {
		parts = append(parts, fmt.Sprintf("%s=%q dirty=%v", draft.Title, draft.Body, draft.DiskExists))
	}
	return strings.Join(parts, " | ")
}
