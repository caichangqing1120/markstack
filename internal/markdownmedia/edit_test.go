package markdownmedia

import (
	"strings"
	"testing"
)

func TestM2ImageReferencePatchUsesExactImageRanges(test *testing.T) {
	source := "![空](<assets/old.png> 'keep')\r\n\r\n> ![图][ref] [普通链接](assets/old.png)\r\n\r\n`![代码](assets/old.png)`\r\n\r\n[ref]: assets/old.png \"title\"\r\n"
	refs := ImageReferences([]byte(source))
	if len(refs) != 2 || refs[0].Destination != "assets/old.png" || refs[1].Destination != "assets/old.png" {
		test.Fatalf("unexpected references: %+v", refs)
	}
	newSource := source
	for index := len(refs) - 1; index >= 0; index-- {
		patch := refs[index].Rename("assets/new.png")
		newSource = newSource[:patch.Start] + patch.Text + newSource[patch.End:]
	}
	want := strings.NewReplacer("<assets/old.png>", "<assets/new.png>", "![图][ref]", `![图](assets/new.png "title")`).Replace(source)
	if newSource != want {
		test.Fatalf("renamed source=%q, want %q", newSource, want)
	}
	for _, percent := range []int{75, 50, 100} {
		patch := ImageReferences([]byte(newSource))[0].SetWidth(newSource, percent)
		newSource = newSource[:patch.Start] + patch.Text + newSource[patch.End:]
		updated := ImageReferences([]byte(newSource))[0]
		if WidthPercent(updated.Title) != percent || !strings.HasPrefix(updated.Title, "keep") || !strings.HasSuffix(newSource, want[strings.Index(want, "\r\n"):]) {
			test.Fatalf("width %d lost title or touched other bytes: %q", percent, newSource)
		}
	}
}
