package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeVisualCompareRequiresMatchedViewport(t *testing.T) {
	for _, command := range []string{"bash", "ffmpeg", "ffprobe", "rg", "shasum"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s unavailable: %v", command, err)
		}
	}
	fixtures := t.TempDir()
	fixture := func(name string, width, height int, ink color.Color) string {
		t.Helper()
		bitmap := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(bitmap, bitmap.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(bitmap, image.Rect(1000, 300, 1200, 340), image.NewUniform(ink), image.Point{}, draw.Src)
		filename := filepath.Join(fixtures, name)
		file, err := os.Create(filename)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, bitmap)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("encode comparison fixture: %v, close: %v", err, closeErr)
		}
		return filename
	}
	reference := fixture("reference.png", 3210, 1910, color.Black)
	candidate := fixture("candidate.png", 3210, 1910, color.RGBA{R: 51, G: 51, B: 51, A: 255})
	wrongSize := fixture("wrong-size.png", 740, 520, color.Black)
	const script = "../../scripts/compare-native-visual.sh"
	outputDir := t.TempDir()
	if output, err := exec.Command("bash", script, reference, candidate, outputDir).CombinedOutput(); err != nil {
		t.Fatalf("compare native windows: %v\n%s", err, output)
	}
	metrics, err := os.ReadFile(filepath.Join(outputDir, "metrics.txt"))
	if err != nil || !strings.Contains(string(metrics), "PSNR") || !strings.Contains(string(metrics), "SSIM") {
		t.Fatalf("missing metrics: %v\n%s", err, metrics)
	}
	difference, err := os.Open(filepath.Join(outputDir, "difference.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer difference.Close()
	config, err := png.DecodeConfig(difference)
	if err != nil || config.Width != 3210 || config.Height != 1910 {
		t.Fatalf("difference image = %+v, %v", config, err)
	}
	invalidDir := filepath.Join(outputDir, "invalid")
	output, err := exec.Command("bash", script, reference, wrongSize, invalidDir).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "3210x1910") {
		t.Fatalf("mismatched viewport accepted: %v\n%s", err, output)
	}
	if _, err := os.Stat(invalidDir); !os.IsNotExist(err) {
		t.Fatalf("invalid comparison wrote output: %v", err)
	}
	lossy := filepath.Join(t.TempDir(), "reference.jpg")
	if output, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", reference, "-frames:v", "1", "-update", "1", lossy).CombinedOutput(); err != nil {
		t.Fatalf("prepare lossy reference: %v\n%s", err, output)
	}
	output, err = exec.Command("bash", script, lossy, candidate, filepath.Join(outputDir, "lossy")).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "PNG") {
		t.Fatalf("lossy reference accepted: %v\n%s", err, output)
	}
}
