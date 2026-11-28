package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/update"
	"markstack/internal/buildinfo"
)

func TestMain(testingMain *testing.M) {
	if handled, err := RunPatchHelper(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(testingMain.Run())
}

func TestHTTPSUpdate(t *testing.T) {
	for _, scenario := range []string{"patch", "fallback", "no-patch", "bad-signature", "cancel-resume", "bad-checksum"} {
		t.Run(scenario, func(t *testing.T) {
			public, private, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			patchBody := fixturePatch(t, scenario == "fallback")
			fullBody := bytes.Repeat([]byte("full archive content"), 128)
			var feed, signature []byte
			var patchHits, fullHits, resumed atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/latest.json":
					_, _ = writer.Write(feed)
				case "/latest.json.sig":
					_, _ = writer.Write(signature)
				case "/delta":
					patchHits.Add(1)
					_, _ = writer.Write(patchBody)
				case "/full":
					attempt := fullHits.Add(1)
					if scenario == "cancel-resume" && attempt == 1 {
						writer.Header().Set("Content-Length", fmt.Sprint(len(fullBody)))
						_, _ = writer.Write(fullBody[:40])
						writer.(http.Flusher).Flush()
						<-request.Context().Done()
						return
					}
					if request.Header.Get("Range") != "" {
						resumed.Add(1)
					}
					body := fullBody
					if scenario == "bad-checksum" {
						body = bytes.Repeat([]byte("x"), len(body))
					}
					http.ServeContent(writer, request, "full", time.Time{}, bytes.NewReader(body))
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()
			format := map[string]string{"darwin": update.FormatZIP, "windows": update.FormatNSIS, "linux": update.FormatTarGz}[runtime.GOOS]
			if format == "" {
				t.Skip("unsupported platform")
			}
			full := update.Asset{OS: runtime.GOOS, Arch: runtime.GOARCH, Format: format, URL: server.URL + "/full", Size: int64(len(fullBody)), SHA256: digest(fullBody)}
			assets := []update.Asset{full}
			wantPatch := scenario == "patch" || scenario == "fallback"
			if wantPatch {
				patchAsset := full
				if runtime.GOOS == "windows" {
					patchAsset.Format, patchAsset.URL = update.FormatZIP, server.URL+"/portable"
				}
				patchAsset.Patches = []update.Patch{{FromVersion: "0.1.0", URL: server.URL + "/delta", Size: int64(len(patchBody)), SHA256: digest(patchBody)}}
				if runtime.GOOS == "windows" {
					assets = append(assets, patchAsset)
				} else {
					assets[0] = patchAsset
				}
			}
			feed, err = json.Marshal(update.Feed{Schema: update.SchemaVersion, App: buildinfo.AppID, Channel: "stable", Latest: update.Release{Version: "0.2.0", Assets: assets}})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "bad-signature" {
				signature = []byte(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)))
			} else {
				signed, err := update.Sign(feed, private)
				if err != nil {
					t.Fatal(err)
				}
				signature = []byte(signed)
			}
			updates := filepath.Join(t.TempDir(), "MarkStack", "updates")
			client, err := New(Config{FeedURL: server.URL + "/latest.json", PublicKey: base64.StdEncoding.EncodeToString(public), Version: "0.1.0", Channel: "stable", Directory: updates, HTTP: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			if wantPatch {
				client.executable = patchHelperExecutable(t)
			}
			offer, err := client.Check(t.Context())
			if scenario == "bad-signature" {
				if !errors.Is(err, update.ErrSignature) || offer != nil {
					t.Fatalf("tampered signature: %+v, %v", offer, err)
				}
				return
			}
			if err != nil || offer == nil || offer.Version != "0.2.0" || offer.FullSize != full.Size || (offer.PatchSize > 0) != wantPatch {
				t.Fatalf("offer = %+v, %v", offer, err)
			}
			if scenario == "cancel-resume" {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				_, err := client.Prepare(ctx, offer, func(progress Progress) {
					if progress.Done > 0 {
						cancel()
					}
				})
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel: %v", err)
				}
			}
			prepared, err := client.Prepare(t.Context(), offer, nil)
			if scenario == "bad-checksum" {
				if !errors.Is(err, update.ErrChecksum) || prepared != nil {
					t.Fatalf("bad checksum: %v, %v", prepared, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if scenario == "patch" {
				payload := prepared.options.Payload
				relative, _ := filepath.Rel(updates, payload)
				body, err := os.ReadFile(filepath.Join(payload, filepath.FromSlash(patchProgram())))
				if payload == "" || strings.HasPrefix(relative, "..") || err != nil || string(body) != "replacement" || fullHits.Load() != 0 {
					t.Fatalf("delta payload=%q body=%q, err=%v; full hits=%d", payload, body, err, fullHits.Load())
				}
			} else if prepared.options.Artifact == "" || prepared.options.Asset.Format != format || fullHits.Load() == 0 {
				t.Fatalf("full not used: %+v", prepared.options)
			}
			if wantPatch && patchHits.Load() != 1 {
				t.Fatalf("patch hits = %d", patchHits.Load())
			}
			if scenario == "cancel-resume" && resumed.Load() != 1 {
				t.Fatal("download did not resume with Range")
			}
		})
	}
}

func digest(body []byte) string { return fmt.Sprintf("%x", sha256.Sum256(body)) }

func patchProgram() string {
	switch runtime.GOOS {
	case "darwin":
		return "Contents/MacOS/markstack"
	case "windows":
		return "markstack.exe"
	default:
		return "markstack"
	}
}

func fixturePatch(t *testing.T, wrongBase bool) []byte {
	t.Helper()
	body := []byte("replacement")
	kind := map[string]string{"darwin": "app", "windows": "dir", "linux": "file"}[runtime.GOOS]
	entry := update.PatchFile{Path: patchProgram(), Mode: 0755, Size: int64(len(body)), SHA256: digest(body), Op: "new", Blob: "files/program"}
	if wrongBase {
		entry.Path, entry.Op, entry.Blob = "missing-old-file", "same", ""
	}
	manifest, err := json.Marshal(update.PatchManifest{Schema: update.PatchSchema, App: buildinfo.AppID, FromVersion: "0.1.0", ToVersion: "0.2.0", OS: runtime.GOOS, Arch: runtime.GOARCH, Payload: kind, Files: []update.PatchFile{entry}})
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(gz)
	write := func(name string, value []byte) {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(value))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	write("patch.json", manifest)
	if !wrongBase {
		write(entry.Blob, body)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func patchHelperExecutable(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "darwin" {
		return self
	}
	root := filepath.Join(t.TempDir(), "MarkStack.app")
	path := filepath.Join(root, filepath.FromSlash(patchProgram()))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestManagedInstallIsNotQuit(t *testing.T) {
	client := &Client{install: func(options update.InstallOptions, quit func()) error {
		return fmt.Errorf("dpkg: %w", update.ErrManagedExternally)
	}}
	prepared := &Prepared{owner: client}
	err := client.Install(prepared, func() { t.Error("external update must not quit") })
	if !errors.Is(err, update.ErrManagedExternally) {
		t.Fatalf("managed install error: %v", err)
	}
}
