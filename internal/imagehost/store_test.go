package imagehost

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestConfigStorePrivateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "imagehost.json")
	t.Setenv("MARKSTACK_IMAGEHOST_PATH", path)
	store, err := DefaultStore()
	if err != nil || store.Path != path {
		t.Fatalf("override: %v, %v", store, err)
	}
	if cfg, err := store.Load(); err != nil || !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Fatalf("missing file should use defaults: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Mode = ModeHTTP
	cfg.HTTP.Endpoint = "https://upload.example"
	cfg.HTTP.Headers = map[string]string{"Authorization": "Bearer example-header"}
	cfg.S3.SecretAccessKey, cfg.S3.PathStyle = "example-secret", false
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || !reflect.DeepEqual(cfg, loaded) {
		t.Fatalf("round trip failed: %v", err)
	}
	if info, err := os.Stat(path); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("wrong file permissions: %v, %v", info, err)
	}
	cfg.Mode = ModeLocal
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 1 {
		t.Fatalf("unexpected leftover files: %v, %v", entries, err)
	}
}

func TestConfigStoreRejectsUnsafeOrCorruptFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store := ConfigStore{Path: filepath.Join(directory, "imagehost.json")}
	target := filepath.Join(directory, "original")
	const original = "private JSON data"
	if err := os.WriteFile(target, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(target, store.Path); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(DefaultConfig()); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("symlink save: %v", err)
		}
		if err := os.Remove(store.Path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.Path, []byte(`{"mode":"local"}`), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("public config permissions: %v", err)
		}
		if err := os.Chmod(store.Path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(store.Path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(DefaultConfig()); !errors.Is(err, ErrCorruptStore) || strings.Contains(err.Error(), original) {
		t.Fatalf("unsafe error or overwrite: %v", err)
	}
	if data, err := os.ReadFile(store.Path); err != nil || string(data) != original {
		t.Fatalf("original file was not preserved: %v", err)
	}
}

func TestConfigStoreDefaultsPathStyleWhenOmitted(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store := ConfigStore{Path: filepath.Join(directory, "imagehost.json")}
	data := []byte(`{"mode":"s3","s3":{"endpoint":"http://localhost:9000","bucket":"bucket","accessKeyID":"test-id","secretAccessKey":"test-key"}}`)
	if err := os.WriteFile(store.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.S3.PathStyle || cfg.S3.Region != "us-east-1" {
		t.Fatal("incomplete S3 configuration did not use the defaults")
	}
}
