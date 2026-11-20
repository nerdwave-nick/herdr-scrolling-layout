package layout

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/nerdwave-nick/herdr-go/api"
)

func rect(x, y, w, h int) api.PaneLayoutRect {
	return api.PaneLayoutRect{X: x, Y: y, Width: w, Height: h}
}

// 2x2 grid built column-first: p1 over p2 on the left, p3 over p4 right.
var grid = api.PaneLayoutSnapshot{
	Area:          rect(0, 0, 100, 40),
	FocusedPaneID: "p1",
	Panes: []api.PaneLayoutPane{
		{PaneID: "p1", Rect: rect(0, 0, 50, 20)},
		{PaneID: "p2", Rect: rect(0, 20, 50, 20)},
		{PaneID: "p3", Rect: rect(50, 0, 50, 20)},
		{PaneID: "p4", Rect: rect(50, 20, 50, 20)},
	},
}

func TestEntryPaneKeepsVerticalPosition(t *testing.T) {
	cases := []struct {
		d    Dir
		ref  float64
		want string
	}{
		{Right, 0.25, "p1"}, // enter at the left edge, upper half
		{Right, 0.75, "p2"}, // left edge, lower half
		{Left, 0.75, "p4"},  // enter at the right edge, lower half
		{Down, 0.8, "p3"},   // enter at the top edge, right half
		{Up, 0.2, "p2"},     // enter at the bottom edge, left half
	}
	for _, c := range cases {
		if got, _ := EntryPane(grid, c.d, c.ref); got != c.want {
			t.Errorf("EntryPane(%s, %.2f) = %s, want %s", c.d, c.ref, got, c.want)
		}
	}
}

func TestEntryPaneZoomedUsesFocused(t *testing.T) {
	z := grid
	z.Zoomed, z.FocusedPaneID = true, "p4"
	if got, _ := EntryPane(z, Right, 0); got != "p4" {
		t.Fatalf("got %s", got)
	}
}

func TestRefFrac(t *testing.T) {
	if f := RefFrac(grid, "p2", Right); math.Abs(f-0.75) > 1e-9 {
		t.Fatalf("got %v", f)
	}
	if f := RefFrac(grid, "p3", Down); math.Abs(f-0.75) > 1e-9 {
		t.Fatalf("got %v", f)
	}
}

func TestColumn(t *testing.T) {
	col := Column(grid, "p4")
	if len(col) != 2 || col[0].PaneID != "p3" || col[1].PaneID != "p4" {
		t.Fatalf("got %+v", col)
	}
}

func TestStackRatios(t *testing.T) {
	r := StackRatios([]int{10, 10, 20})
	want := []float64{0.25, 1.0 / 3}
	for i := range want {
		if math.Abs(r[i]-want[i]) > 1e-9 {
			t.Fatalf("ratios %v, want %v", r, want)
		}
	}
	if StackRatios([]int{5}) != nil {
		t.Fatal("single pane needs no ratios")
	}
}

func tree(t *testing.T, s string) api.LayoutNode {
	t.Helper()
	var n api.LayoutNode
	if err := json.Unmarshal([]byte(s), &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestFindGroupAndInsert(t *testing.T) {
	root := tree(t, `{"type":"split","direction":"right","ratio":0.4,
		"first":{"type":"split","direction":"down","ratio":0.5,"first":{"type":"pane","pane_id":"p1"},"second":{"type":"pane","pane_id":"p2"}},
		"second":{"type":"split","direction":"down","ratio":0.3,"first":{"type":"pane","pane_id":"p3"},"second":{"type":"pane","pane_id":"p4"}}}`)
	p, sub, ok := FindGroup(root, []string{"p3", "p4"})
	if !ok {
		t.Fatal("right column not found")
	}
	if p.Split != api.SplitDirectionRight || p.Before || p.Ratio != 0.4 || !equal(p.SiblingLeaves, "p1", "p2") {
		t.Fatalf("placement %+v", p)
	}
	if !equal(Leaves(sub), "p3", "p4") {
		t.Fatalf("subtree %v", Leaves(sub))
	}

	// Lifting leaves the left column; inserting restores the exact tree.
	rest := tree(t, `{"type":"split","direction":"down","ratio":0.5,"first":{"type":"pane","pane_id":"p1"},"second":{"type":"pane","pane_id":"p2"}}`)
	got, _ := json.Marshal(Insert(rest, p, sub))
	want, _ := json.Marshal(root)
	if string(got) != string(want) {
		t.Fatalf("insert:\n got %s\nwant %s", got, want)
	}

	if _, _, ok := FindGroup(root, []string{"p1", "p3"}); ok {
		t.Fatal("a row is not a subtree of a column-first layout")
	}
}

func TestInsertNextToCoveringSubtree(t *testing.T) {
	// The sibling side was [p1 p2]; since lifting, p2 closed and p5 appeared
	// beside p1. The group goes next to what covers the surviving sibling.
	rest := tree(t, `{"type":"split","direction":"down","ratio":0.5,"first":{"type":"pane","pane_id":"p1"},"second":{"type":"pane","pane_id":"p5"}}`)
	p := Placement{SiblingLeaves: []string{"p1", "p2"}, Split: api.SplitDirectionRight, Before: true, Ratio: 0.3}
	out := Insert(rest, p, Pane("p9"))
	s := out.Value.(api.LayoutNodeSplit)
	if s.Direction != api.SplitDirectionDown || !equal(Leaves(out), "p9", "p1", "p5") {
		t.Fatalf("got %v (%s)", Leaves(out), s.Direction)
	}
	inner := s.First.Value.(api.LayoutNodeSplit)
	if inner.Ratio != 0.3 || !equal(Leaves(inner.First), "p9") {
		t.Fatalf("inner %+v", inner)
	}
}

func equal(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
