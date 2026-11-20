package layout

import "github.com/nerdwave-nick/herdr-go/api"

// Placement records where a lifted group sat in its tab's layout tree, so it
// can be re-inserted exactly.
type Placement struct {
	// SiblingLeaves are the panes on the other side of the group's parent
	// split. On restore the group goes next to the smallest subtree that
	// covers whichever of them still exist.
	SiblingLeaves []string `json:"sibling_leaves"`
	// Split is the parent split's direction.
	Split api.SplitDirection `json:"split"`
	// Before is true when the group was the first child (left / above).
	Before bool `json:"before"`
	// Ratio is the parent split's ratio (share of the first child).
	Ratio float64 `json:"ratio"`
}

// Leaves returns the pane ids of a layout subtree in order.
func Leaves(n api.LayoutNode) []string {
	switch v := n.Value.(type) {
	case api.LayoutNodePane:
		return []string{v.PaneID}
	case api.LayoutNodeSplit:
		return append(Leaves(v.First), Leaves(v.Second)...)
	}
	return nil
}

// Pane returns a leaf node.
func Pane(id string) api.LayoutNode { return api.LayoutNode{Value: api.LayoutNodePane{PaneID: id}} }

// FindGroup locates the subtree whose leaves are exactly group and returns
// it with its placement. ok is false when the group is not a subtree or is
// the whole tree.
func FindGroup(root api.LayoutNode, group []string) (Placement, api.LayoutNode, bool) {
	want := set(group)
	var walk func(n api.LayoutNode) (Placement, api.LayoutNode, bool)
	walk = func(n api.LayoutNode) (Placement, api.LayoutNode, bool) {
		s, ok := n.Value.(api.LayoutNodeSplit)
		if !ok {
			return Placement{}, api.LayoutNode{}, false
		}
		children := [2]api.LayoutNode{s.First, s.Second}
		for i, child := range children {
			if sameSet(Leaves(child), want) {
				return Placement{
					SiblingLeaves: Leaves(children[1-i]),
					Split:         s.Direction,
					Before:        i == 0,
					Ratio:         s.Ratio,
				}, child, true
			}
		}
		if p, g, ok := walk(s.First); ok {
			return p, g, true
		}
		return walk(s.Second)
	}
	return walk(root)
}

// Covering returns the smallest subtree containing every pane of ids that
// is present in root. ok is false when none are present.
func Covering(root api.LayoutNode, ids []string) (api.LayoutNode, bool) {
	present := map[string]bool{}
	for _, l := range Leaves(root) {
		present[l] = true
	}
	want := map[string]bool{}
	for _, id := range ids {
		if present[id] {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return api.LayoutNode{}, false
	}
	n := root
	for {
		s, ok := n.Value.(api.LayoutNodeSplit)
		if !ok {
			return n, true
		}
		switch {
		case containsAll(Leaves(s.First), want):
			n = s.First
		case containsAll(Leaves(s.Second), want):
			n = s.Second
		default:
			return n, true
		}
	}
}

// Insert puts group back next to the subtree covering p.SiblingLeaves (or
// the whole tree if none remain), using the recorded split.
func Insert(root api.LayoutNode, p Placement, group api.LayoutNode) api.LayoutNode {
	anchor, ok := Covering(root, p.SiblingLeaves)
	if !ok {
		anchor = root
	}
	split := api.LayoutNodeSplit{Direction: p.Split, Ratio: p.Ratio, First: anchor, Second: group}
	if p.Before {
		split.First, split.Second = group, anchor
	}
	return replace(root, set(Leaves(anchor)), api.LayoutNode{Value: split})
}

// replace swaps the subtree whose leaf set equals target for with.
func replace(n api.LayoutNode, target map[string]bool, with api.LayoutNode) api.LayoutNode {
	if sameSet(Leaves(n), target) {
		return with
	}
	if s, ok := n.Value.(api.LayoutNodeSplit); ok {
		s.First = replace(s.First, target, with)
		s.Second = replace(s.Second, target, with)
		return api.LayoutNode{Value: s}
	}
	return n
}

// Stack returns a vertical stack of panes with the given heights.
func Stack(ids []string, heights []int) api.LayoutNode {
	if len(ids) == 1 {
		return Pane(ids[0])
	}
	ratios := StackRatios(heights)
	return api.LayoutNode{Value: api.LayoutNodeSplit{
		Direction: api.SplitDirectionDown,
		Ratio:     ratios[0],
		First:     Pane(ids[0]),
		Second:    Stack(ids[1:], heights[1:]),
	}}
}

func set(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func sameSet(ids []string, want map[string]bool) bool {
	if len(ids) != len(want) {
		return false
	}
	return containsAll(ids, want)
}

func containsAll(ids []string, want map[string]bool) bool {
	have := set(ids)
	for id := range want {
		if !have[id] {
			return false
		}
	}
	return true
}
