//go:build darwin

package main

import (
	"bufio"
	"context"
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

func TestRecoveryJournalSurvivesKilledHeadlessProcess(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	store := session.RecoveryStore{Path: filepath.Join(home, "Library", "Application Support", "MarkStack", "recovery.json")}
	binary := filepath.Join(root, "markstack-headless")
	build := exec.Command("go", "build", "-tags", "vivahead", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build headless app: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	launch := func() (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
		command := exec.CommandContext(ctx, binary)
		command.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "VIVA_HEADLESS=1", "MARKSTACK_RECOVERY_PATH=" + store.Path}
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
		line, err := reader.ReadString('\n')
		if err != nil || !strings.Contains(line, "ok ready") {
			t.Fatalf("headless startup=%q err=%v", line, err)
		}
		return command, input, reader
	}
	first, input, reader := launch()
	defer first.Process.Kill()
	if _, err := io.WriteString(input, "type Recovery draft\n"); err != nil {
		t.Fatal(err)
	}
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "ok") {
		t.Fatalf("headless typing=%q err=%v", line, err)
	}
	deadline := time.After(5 * time.Second)
	for {
		snapshot, err := store.Load()
		if err == nil && len(snapshot.Drafts) == 1 && snapshot.Drafts[0].Body == "Recovery draft" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("draft was not journaled: snapshot=%+v err=%v", snapshot, err)
		case <-time.After(10 * time.Millisecond):
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
	if _, err := io.WriteString(input, "settle 100\neditor\n"); err != nil {
		t.Fatal(err)
	}
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "ok") {
		t.Fatalf("recovered settle=%q err=%v", line, err)
	}
	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, fmt.Sprintf("value=%q", "Recovery draft")) {
		t.Fatalf("recovered editor=%q err=%v", line, err)
	}
}
