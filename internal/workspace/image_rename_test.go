package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestM2ImageRenameConfinesPathsAndDoesNotOverwrite(test *testing.T) {
	root := test.TempDir()
	document := filepath.Join(root, "note.md")
	assets := filepath.Join(root, "assets")
	if err := os.WriteFile(document, nil, 0600); err != nil {
		test.Fatal(err)
	}
	location, err := CanonicalLocation(document)
	if err != nil {
		test.Fatal(err)
	}
	if err := os.Mkdir(assets, 0700); err != nil {
		test.Fatal(err)
	}
	for _, name := range []string{"original.png", "occupied.png"} {
		if err := os.WriteFile(filepath.Join(assets, name), []byte(name), 0600); err != nil {
			test.Fatal(err)
		}
	}
	for _, name := range []string{"../escape", `..\escape`, "/absolute", `C:\absolute`, "CON.png", "pic.png:stream"} {
		if _, err := ImageAssetName("assets/original.png", name); !errors.Is(err, ErrOutsideWorkspace) {
			test.Fatalf("name %q accepted: %v", name, err)
		}
	}
	if _, err := RenameImageAsset(root, document, location, "assets/original.png", "assets/occupied.png", nil); !errors.Is(err, ErrRenameTargetExists) {
		test.Fatalf("occupied target was not refused: %v", err)
	}
	other := test.TempDir()
	if _, err := RenameImageAsset(other, document, location, "assets/original.png", "assets/renamed.png", nil); !errors.Is(err, ErrOutsideWorkspace) {
		test.Fatalf("workspace boundary was not enforced: %v", err)
	}
	if err := os.Symlink(filepath.Join(assets, "original.png"), filepath.Join(assets, "linked.png")); err == nil {
		if _, err := RenameImageAsset(root, document, location, "assets/linked.png", "assets/renamed.png", nil); !errors.Is(err, ErrOutsideWorkspace) {
			test.Fatalf("symlink was followed: %v", err)
		}
	}
	if data, err := ReadAsset(root, document, location, "assets/original.png"); err != nil || string(data) != "original.png" {
		test.Fatalf("slash path does not resolve: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(assets, "occupied.png")); err != nil || string(data) != "occupied.png" {
		test.Fatalf("collision changed existing file: %q %v", data, err)
	}
}
