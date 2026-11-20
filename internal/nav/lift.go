package nav

import (
	"context"
	"fmt"

	"github.com/nerdwave-nick/herdr-go/api"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/layout"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/state"
)

// niri has two kinds of fullscreen, and so does this plugin. Both "lift" a
// group of panes out of their tab into a new tab placed right after it in
// the strip, and both are toggles that put the group back:
//
//   - MaximizeColumn lifts the focused pane's whole column. In a 2x2 layout
//     you get a tab with that column's two panes, full width, stacked.
//   - FullscreenWindow lifts just the focused pane.
//
// Lifting records the group's parent split (direction, ratio, side, and the
// panes on the other side). Restoring re-inserts the lifted tab's current
// tree into the origin tab's current tree and rebuilds the origin tab to
// match. pane.move keeps processes alive, so nothing restarts.

// MaximizeColumn toggles the focused column between its tab and its own tab.
func (n *Nav) MaximizeColumn(ctx context.Context) error { return n.toggle(ctx, state.KindColumn) }

// FullscreenWindow toggles the focused pane between its tab and its own tab.
func (n *Nav) FullscreenWindow(ctx context.Context) error { return n.toggle(ctx, state.KindWindow) }

func (n *Nav) toggle(ctx context.Context, kind string) error {
	paneID, err := n.current(ctx)
	if err != nil {
		return err
	}
	l, err := n.paneLayout(ctx, paneID)
	if err != nil {
		return err
	}
	if l.Zoomed {
		return fmt.Errorf("tab is zoomed; unzoom first")
	}
	if lift, ok := n.State.Lifts[l.TabID]; ok {
		return n.restore(ctx, l, paneID, lift)
	}
	var group []string
	if kind == state.KindColumn {
		for _, p := range layout.Column(l, paneID) {
			group = append(group, p.PaneID)
		}
	} else {
		group = []string{paneID}
	}
	if len(group) == len(l.Panes) {
		return nil // already alone in its tab: nothing to lift
	}
	root, err := n.tabTree(ctx, l.TabID)
	if err != nil {
		return err
	}
	place, sub, ok := layout.FindGroup(root, group)
	if !ok && kind == state.KindColumn {
		// The column is not a subtree (rows were split first): lift just the
		// focused window instead.
		group, kind = []string{paneID}, state.KindWindow
		place, sub, ok = layout.FindGroup(root, group)
	}
	if !ok {
		return fmt.Errorf("cannot locate pane %s in its tab layout", paneID)
	}
	return n.lift(ctx, l, paneID, kind, sub, place)
}

func (n *Nav) lift(ctx context.Context, l api.PaneLayoutSnapshot, focus, kind string, group api.LayoutNode, place layout.Placement) error {
	tabs, err := n.tabs(ctx, l.WorkspaceID)
	if err != nil {
		return err
	}
	origin := indexOfTab(tabs, l.TabID)

	ids := layout.Leaves(group)
	res, err := n.moveToNewTab(ctx, ids[0], l.WorkspaceID, false)
	if err != nil {
		return err
	}
	newTab := res.Pane.TabID
	if err := n.arrange(ctx, newTab, ids[0], group); err != nil {
		return err
	}
	if origin >= 0 {
		if err := n.moveTab(ctx, newTab, origin+1); err != nil {
			return err
		}
	}
	n.State.Lifts[newTab] = state.Lift{Kind: kind, WorkspaceID: l.WorkspaceID, OriginTab: l.TabID, Placement: place}
	if err := n.focusTab(ctx, newTab); err != nil {
		return err
	}
	return n.focusPane(ctx, focus)
}

func (n *Nav) restore(ctx context.Context, l api.PaneLayoutSnapshot, focus string, lift state.Lift) error {
	group, err := n.tabTree(ctx, l.TabID)
	if err != nil {
		return err
	}
	originRoot, err := n.tabTree(ctx, lift.OriginTab)
	if err != nil {
		// The origin tab is gone. Forget the lift; the panes stay where they are.
		delete(n.State.Lifts, l.TabID)
		return nil
	}
	delete(n.State.Lifts, l.TabID)
	target := layout.Insert(originRoot, lift.Placement, group)

	// Rebuild the origin tab in place: keep one of its panes as the seed and
	// park the others in a scratch tab, then arrange everything.
	originPanes := layout.Leaves(originRoot)
	keep := originPanes[0]
	if len(originPanes) > 1 {
		res, err := n.moveToNewTab(ctx, originPanes[1], l.WorkspaceID, false)
		if err != nil {
			return err
		}
		for _, id := range originPanes[2:] {
			if _, err := n.moveToTab(ctx, id, res.Pane.TabID, "", api.SplitDirectionRight, 0); err != nil {
				return err
			}
		}
	}
	if err := n.arrange(ctx, lift.OriginTab, keep, target); err != nil {
		return err
	}
	if err := n.focusTab(ctx, lift.OriginTab); err != nil {
		return err
	}
	return n.focusPane(ctx, focus)
}

// arrange shapes tab, which currently holds only seed, into tree by moving
// the tree's other panes in, then swaps panes into their final positions.
// Swaps keep the split shape and ratios. Source tabs that empty out are
// closed by Herdr.
func (n *Nav) arrange(ctx context.Context, tab, seed string, tree api.LayoutNode) error {
	want := map[string]string{} // slot (named by the pane that opened it) -> pane that belongs there
	placed := map[string]bool{seed: true}
	var order []string // slots in tree order, for deterministic swaps

	// pick prefers the pane that belongs in the new slot, to save swaps.
	pick := func(node api.LayoutNode) string {
		for _, id := range layout.Leaves(node) {
			if !placed[id] {
				return id
			}
		}
		for _, id := range layout.Leaves(tree) {
			if !placed[id] {
				return id
			}
		}
		return ""
	}
	var build func(node api.LayoutNode, holder string) error
	build = func(node api.LayoutNode, holder string) error {
		switch v := node.Value.(type) {
		case api.LayoutNodePane:
			want[holder] = v.PaneID
			order = append(order, holder)
			return nil
		case api.LayoutNodeSplit:
			next := pick(v.Second)
			if next == "" {
				return fmt.Errorf("arrange: ran out of panes")
			}
			placed[next] = true
			if _, err := n.moveToTab(ctx, next, tab, holder, v.Direction, v.Ratio); err != nil {
				return err
			}
			if err := build(v.First, holder); err != nil {
				return err
			}
			return build(v.Second, next)
		}
		return fmt.Errorf("arrange: unknown layout node %T", node.Value)
	}
	if err := build(tree, seed); err != nil {
		return err
	}

	// occupant[slot] is the pane currently in slot; slotOf is the inverse.
	occupant := map[string]string{}
	slotOf := map[string]string{}
	for _, s := range order {
		occupant[s], slotOf[s] = s, s
	}
	for _, s := range order {
		cur, w := occupant[s], want[s]
		if cur == w {
			continue
		}
		ws := slotOf[w]
		if err := n.swap(ctx, cur, w); err != nil {
			return err
		}
		occupant[s], occupant[ws] = w, cur
		slotOf[w], slotOf[cur] = s, ws
	}
	return nil
}
