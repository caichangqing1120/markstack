package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkbenchScreenshotCompareAllowsOnlyWorkspacePathPixels(t *testing.T) {
	for _, command := range []string{"bash", "ffmpeg", "ffprobe", "cmp"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s unavailable: %v", command, err)
		}
	}
	const script = "../../scripts/compare-workbench-screenshot.sh"
	directory := t.TempDir()
	fixture := func(name string, width, height int, point image.Point, lossy bool) string {
		t.Helper()
		bitmap := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(bitmap, bitmap.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		bitmap.Set(point.X, point.Y, color.Black)
		path := filepath.Join(directory, name)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if lossy {
			err = jpeg.Encode(file, bitmap, nil)
		} else {
			err = png.Encode(file, bitmap)
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("encode fixture: %v, close: %v", err, closeErr)
		}
		return path
	}
	reference := fixture("reference.png", 740, 520, image.Pt(230, 505), false)
	pathOnly := fixture("path-only.png", 740, 520, image.Pt(240, 505), false)
	if output, err := exec.Command("bash", script, reference, pathOnly).CombinedOutput(); err != nil {
		t.Fatalf("workspace-path-only change rejected: %v\n%s", err, output)
	}
	for _, test := range []struct {
		name   string
		width  int
		height int
		point  image.Point
		lossy  bool
		want   string
	}{
		{"body", 740, 520, image.Pt(300, 100), false, "pixels differ"},
		{"status-rule", 740, 520, image.Pt(230, 494), false, "pixels differ"},
		{"status-count", 740, 520, image.Pt(650, 505), false, "pixels differ"},
		{"wrong-size", 741, 520, image.Pt(230, 505), false, "740x520"},
		{"lossy", 740, 520, image.Pt(230, 505), true, "PNG"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := fixture(test.name+".png", test.width, test.height, test.point, test.lossy)
			output, err := exec.Command("bash", script, reference, candidate).CombinedOutput()
			if err == nil || !strings.Contains(string(output), test.want) {
				t.Fatalf("invalid screenshot accepted or wrong failure: %v\n%s", err, output)
			}
		})
	}
}

func TestCurrentUIScreenshotCheckDoesNotNeedGolden(test *testing.T) {
	for _, command := range []string{"node", "ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(command); err != nil {
			test.Skipf("%s unavailable: %v", command, err)
		}
	}
	for _, scenario := range []struct {
		name          string
		width, height int
		blank, alpha  bool
		lossy         bool
		valid         bool
		failure       string
	}{
		{"current", 740, 520, false, false, false, true, ""},
		{"blank-document", 740, 520, true, false, false, false, "no visible content"},
		{"transparent-document", 740, 520, false, true, false, false, "transparent"},
		{"wrong-size", 741, 520, false, false, false, false, "width differs"},
		{"lossy", 740, 520, false, false, true, false, "lossless PNG"},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			bitmap := image.NewRGBA(image.Rect(0, 0, scenario.width, scenario.height))
			draw.Draw(bitmap, bitmap.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
			for left := 100; left < 700; left += 40 {
				draw.Draw(bitmap, image.Rect(left, 10, left+10, 20), image.NewUniform(color.Black), image.Point{}, draw.Src)
			}
			if !scenario.blank {
				for row := 120; row < 220; row += 20 {
					draw.Draw(bitmap, image.Rect(230, row, 450, row+2), image.NewUniform(color.Black), image.Point{}, draw.Src)
				}
			}
			if scenario.alpha {
				draw.Draw(bitmap, image.Rect(220, 110, 710, 494), image.Transparent, image.Point{}, draw.Src)
			}
			filename := filepath.Join(context.TempDir(), "current.png")
			file, err := os.Create(filename)
			if err != nil {
				context.Fatal(err)
			}
			if scenario.lossy {
				err = jpeg.Encode(file, bitmap, nil)
			} else {
				err = png.Encode(file, bitmap)
			}
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				context.Fatalf("encode fixture: %v, close: %v", err, closeErr)
			}
			output, err := exec.Command("node", "../../scripts/check-ui-screenshot.cjs", filename, "740", "520").CombinedOutput()
			if scenario.valid {
				if err != nil || !strings.Contains(string(output), "PNG health") {
					context.Fatalf("current screenshot needs a historical golden: %v\n%s", err, output)
				}
			} else if err == nil || !strings.Contains(string(output), scenario.failure) {
				context.Fatalf("invalid current screenshot accepted or failed for another reason: %v\n%s", err, output)
			}
		})
	}
}
