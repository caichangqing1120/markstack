package session

import "testing"

func TestCloseReleasesVacatedTabStorage(test *testing.T) {
	for _, scenario := range []struct {
		name  string
		index int
	}{
		{name: "middle", index: 1},
		{name: "last", index: 2},
	} {
		test.Run(scenario.name, func(subtest *testing.T) {
			state := New()
			state.Tabs = make([]*Document, 0, 3)
			first, middle, last := state.NewDocument(), state.NewDocument(), state.NewDocument()
			state.Active = 0
			previous := state.Tabs
			if !state.RequestClose(scenario.index) {
				subtest.Fatal("clean tab did not close")
			}
			remaining := last
			if scenario.index == 2 {
				remaining = middle
			}
			if len(state.Tabs) != 2 || state.Current() != first || state.Tabs[1] != remaining {
				subtest.Fatal("close changed surviving tab order or the active tab")
			}
			if retained := previous[len(state.Tabs)]; retained != nil {
				subtest.Fatalf("vacated tab slot retains document %d", retained.ID)
			}
		})
	}
}
