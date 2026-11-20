// Package nav implements the niri-style navigation model on top of Herdr:
//
//   - workspaces are stacked vertically, like niri workspaces;
//   - the tabs of a workspace form the horizontal scrolling strip;
//   - panes are the windows tiled inside a tab;
//   - a tab holding one lifted pane or column is a fullscreen/maximized view.
//
// Every movement first tries to stay inside the current tab and only crosses
// into the neighbouring tab (left/right) or workspace (up/down) at the edge.
//
// Actions run as one-shot processes per key press, so they query exactly what
// they need instead of loading the whole session.
package nav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	herdr "github.com/nerdwave-nick/herdr-go"
	"github.com/nerdwave-nick/herdr-go/api"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/config"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/layout"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/state"
)

// Nav carries everything one action needs.
type Nav struct {
	C     *herdr.Client
	Cfg   config.Config
	State *state.Store
	// PaneID is the pane the action applies to. Empty means the server's
	// focused pane.
	PaneID string
}

// FromEnv returns the pane id from the plugin invocation context, if any.
func FromEnv() string { return os.Getenv("HERDR_PANE_ID") }

func (n *Nav) current(ctx context.Context) (string, error) {
	if n.PaneID != "" {
		return n.PaneID, nil
	}
	r, err := n.C.PaneCurrent(ctx, api.PaneCurrentParams{})
	if err != nil {
		return "", err
	}
	return r.Pane.PaneID, nil
}

func (n *Nav) tabs(ctx context.Context, wsID string) ([]api.TabInfo, error) {
	r, err := n.C.TabList(ctx, api.TabListParams{WorkspaceID: wsID})
	return r.Tabs, err // server returns display order; Number is not a position
}

func (n *Nav) workspaces(ctx context.Context) ([]api.WorkspaceInfo, error) {
	r, err := n.C.WorkspaceList(ctx)
	return r.Workspaces, err
}

func (n *Nav) paneLayout(ctx context.Context, paneID string) (api.PaneLayoutSnapshot, error) {
	r, err := n.C.PaneLayout(ctx, api.PaneLayoutParams{PaneID: paneID})
	return r.Layout, err
}

// tabLayout returns the geometric layout of any tab.
func (n *Nav) tabLayout(ctx context.Context, tabID string) (api.PaneLayoutSnapshot, error) {
	root, err := n.tabTree(ctx, tabID)
	if err != nil {
		return api.PaneLayoutSnapshot{}, err
	}
	leaves := layout.Leaves(root)
	if len(leaves) == 0 {
		return api.PaneLayoutSnapshot{}, fmt.Errorf("tab %s has no panes", tabID)
	}
	return n.paneLayout(ctx, leaves[0])
}

func (n *Nav) tabTree(ctx context.Context, tabID string) (api.LayoutNode, error) {
	r, err := n.C.LayoutExport(ctx, api.LayoutExportParams{TabID: tabID})
	return r.Layout.Root, err
}

func (n *Nav) focusPane(ctx context.Context, id string) error {
	_, err := n.C.PaneFocus(ctx, api.PaneTarget{PaneID: id})
	return err
}

func (n *Nav) focusTab(ctx context.Context, id string) error {
	_, err := n.C.TabFocus(ctx, api.TabTarget{TabID: id})
	return err
}

func (n *Nav) moveTab(ctx context.Context, id string, index int) error {
	_, err := n.C.TabMove(ctx, api.TabMoveParams{TabID: id, InsertIndex: index})
	return err
}

func (n *Nav) swap(ctx context.Context, source, target string) error {
	_, err := n.C.PaneSwap(ctx, api.PaneSwapParams{SourcePaneID: source, TargetPaneID: target})
	return err
}

func (n *Nav) moveToTab(ctx context.Context, paneID, tabID, target string, split api.SplitDirection, ratio float64) (api.PaneMoveResult, error) {
	r, err := n.C.PaneMove(ctx, api.PaneMoveParams{
		PaneID: paneID,
		Destination: api.PaneMoveDestination{Value: api.PaneMoveDestinationTab{
			TabID: tabID, TargetPaneID: target, Split: split, Ratio: ratio,
		}},
	})
	return r.MoveResult, err
}

func (n *Nav) moveToNewTab(ctx context.Context, paneID, wsID string, focus bool) (api.PaneMoveResult, error) {
	r, err := n.C.PaneMove(ctx, api.PaneMoveParams{
		PaneID:      paneID,
		Focus:       focus,
		Destination: api.PaneMoveDestination{Value: api.PaneMoveDestinationNewTab{WorkspaceID: wsID}},
	})
	return r.MoveResult, err
}

func indexOfTab(tabs []api.TabInfo, id string) int {
	for i, t := range tabs {
		if t.TabID == id {
			return i
		}
	}
	return -1
}

func indexOfWorkspace(ws []api.WorkspaceInfo, id string) int {
	for i, w := range ws {
		if w.WorkspaceID == id {
			return i
		}
	}
	return -1
}

// step returns i moved one slot in d's sense.
func step(i int, d layout.Dir) int {
	if layout.Forward(d) {
		return i + 1
	}
	return i - 1
}

// ---- focus ----------------------------------------------------------------

// Focus moves focus one window in d, crossing into the neighbouring tab or
// workspace at the edge of the current tab.
func (n *Nav) Focus(ctx context.Context, d layout.Dir) error {
	paneID, err := n.current(ctx)
	if err != nil {
		return err
	}
	er, err := n.C.PaneEdges(ctx, api.PaneEdgesParams{PaneID: paneID})
	if err != nil {
		return err
	}
	e := er.Edges
	if !e.Layout.Zoomed && !layout.AtEdge(e, d) {
		_, err := n.C.PaneFocusDirection(ctx, api.PaneFocusDirectionParams{PaneID: paneID, Direction: d})
		return err
	}
	if layout.Horizontal(d) {
		if !n.Cfg.CrossTabs {
			return nil
		}
		return n.focusAdjacentTab(ctx, e.Layout, paneID, d)
	}
	if !n.Cfg.CrossWorkspaces {
		return nil
	}
	return n.focusAdjacentWorkspace(ctx, e.Layout.WorkspaceID, d)
}

func (n *Nav) focusAdjacentTab(ctx context.Context, l api.PaneLayoutSnapshot, paneID string, d layout.Dir) error {
	tabs, err := n.tabs(ctx, l.WorkspaceID)
	if err != nil {
		return err
	}
	i := indexOfTab(tabs, l.TabID)
	j := step(i, d)
	if i < 0 || j < 0 || j >= len(tabs) {
		return nil // end of the strip
	}
	ref := layout.RefFrac(l, paneID, d)
	tl, err := n.tabLayout(ctx, tabs[j].TabID)
	if err != nil {
		return err
	}
	if err := n.focusTab(ctx, tabs[j].TabID); err != nil {
		return err
	}
	target, ok := layout.EntryPane(tl, d, ref)
	if !ok || target == tl.FocusedPaneID {
		return nil
	}
	return n.focusPane(ctx, target)
}

func (n *Nav) focusAdjacentWorkspace(ctx context.Context, wsID string, d layout.Dir) error {
	wss, err := n.workspaces(ctx)
	if err != nil {
		return err
	}
	i := indexOfWorkspace(wss, wsID)
	if i < 0 {
		return fmt.Errorf("workspace %s not found", wsID)
	}
	j := step(i, d)
	if j < 0 || j >= len(wss) {
		_, err := n.emptySlot(ctx, wss[i], d, true)
		return err
	}
	if _, err := n.C.WorkspaceFocus(ctx, api.WorkspaceTarget{WorkspaceID: wss[j].WorkspaceID}); err != nil {
		return err
	}
	n.cleanup(ctx, wss[i].WorkspaceID)
	return nil
}

// emptySlot creates a fresh workspace past the end of the stack in d, if
// configured, and focuses it. It never stacks a second empty slot onto one
// the plugin already created. With snapshot, it records the settled screen
// so the slot can be removed again if left untouched.
func (n *Nav) emptySlot(ctx context.Context, cur api.WorkspaceInfo, d layout.Dir, snapshot bool) (string, error) {
	if (d == layout.Down && !n.Cfg.EmptyWorkspaceBelow) || (d == layout.Up && !n.Cfg.EmptyWorkspaceAbove) {
		return "", nil
	}
	if _, ok := n.State.EmptySlots[cur.WorkspaceID]; ok && n.untouched(ctx, cur.WorkspaceID, false) {
		return "", nil // already sitting in the empty slot
	}
	r, err := n.C.WorkspaceCreate(ctx, api.WorkspaceCreateParams{Focus: true, SourceWorkspaceID: cur.WorkspaceID})
	if err != nil {
		return "", err
	}
	id := r.Workspace.WorkspaceID
	if d == layout.Up {
		if _, err := n.C.WorkspaceMove(ctx, api.WorkspaceMoveParams{WorkspaceID: id, InsertIndex: 0}); err != nil {
			return id, err
		}
	}
	slot := state.EmptySlot{Pane: r.RootPane.PaneID}
	if snapshot {
		slot.Screen = n.settledScreen(ctx, slot.Pane)
	}
	n.State.EmptySlots[id] = slot
	n.cleanup(ctx, cur.WorkspaceID)
	return id, nil
}

// settledScreen waits for a new shell to finish drawing its prompt and
// returns a hash of the visible text, or "" if it does not settle in time.
func (n *Nav) settledScreen(ctx context.Context, pane string) string {
	deadline := time.Now().Add(settleTimeout)
	prev := ""
	for time.Now().Before(deadline) {
		if n.shellForeground(ctx, pane) {
			h := n.screenHash(ctx, pane)
			if h != "" && h == prev {
				return h // unchanged across two reads: settled
			}
			prev = h
		}
		time.Sleep(settleInterval)
	}
	return ""
}

const (
	settleTimeout  = 2 * time.Second
	settleInterval = 60 * time.Millisecond
)

// screenHash hashes the pane's visible text; "" if empty or unreadable.
func (n *Nav) screenHash(ctx context.Context, pane string) string {
	r, err := n.C.PaneRead(ctx, api.PaneReadParams{PaneID: pane, Source: api.ReadSourceVisible})
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(r.Read.Text)
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (n *Nav) shellForeground(ctx context.Context, pane string) bool {
	pi, err := n.C.PaneProcessInfo(ctx, api.PaneProcessInfoParams{PaneID: pane})
	if err != nil {
		return false
	}
	p := pi.ProcessInfo
	return p.ShellPID != 0 && p.ForegroundProcessGroupID == p.ShellPID
}

// untouched reports whether a plugin-created workspace still holds only its
// initial shell. With strict, the screen must also be unchanged since it was
// created, so any command run in it (even one that has finished) counts as
// touching it. Without a snapshot, strict is never satisfied.
func (n *Nav) untouched(ctx context.Context, wsID string, strict bool) bool {
	slot, ok := n.State.EmptySlots[wsID]
	if !ok {
		return false
	}
	ws, err := n.C.WorkspaceGet(ctx, api.WorkspaceTarget{WorkspaceID: wsID})
	if err != nil || ws.Workspace.TabCount != 1 || ws.Workspace.PaneCount != 1 {
		return false
	}
	root, err := n.tabTree(ctx, ws.Workspace.ActiveTabID)
	if err != nil {
		return false
	}
	if leaves := layout.Leaves(root); len(leaves) != 1 || leaves[0] != slot.Pane {
		return false
	}
	if !n.shellForeground(ctx, slot.Pane) {
		return false
	}
	if !strict {
		return true
	}
	return slot.Screen != "" && n.screenHash(ctx, slot.Pane) == slot.Screen
}

// cleanup closes a plugin-created workspace that was left untouched, like
// niri dropping an empty workspace you navigate away from.
func (n *Nav) cleanup(ctx context.Context, wsID string) {
	if _, ok := n.State.EmptySlots[wsID]; !ok {
		return
	}
	if n.Cfg.CleanupEmptyWorkspaces && n.untouched(ctx, wsID, true) {
		if _, err := n.C.WorkspaceClose(ctx, api.WorkspaceCloseParams{WorkspaceID: wsID}); err != nil {
			return
		}
	}
	// Closed, or the user started using it: either way no longer ours.
	delete(n.State.EmptySlots, wsID)
}

// ---- move -----------------------------------------------------------------

// Move moves the focused window one slot in d. Inside a tab it swaps with
// the neighbour; at the edge it carries the window into the adjacent tab or
// workspace, opening a new tab at the end of the strip when there is none.
func (n *Nav) Move(ctx context.Context, d layout.Dir) error {
	paneID, err := n.current(ctx)
	if err != nil {
		return err
	}
	er, err := n.C.PaneEdges(ctx, api.PaneEdgesParams{PaneID: paneID})
	if err != nil {
		return err
	}
	e := er.Edges
	if e.Layout.Zoomed {
		return errors.New("tab is zoomed; unzoom before moving windows")
	}
	if !layout.AtEdge(e, d) {
		_, err := n.C.PaneSwap(ctx, api.PaneSwapParams{PaneID: paneID, Direction: d})
		return err
	}
	if layout.Horizontal(d) {
		return n.moveAcrossTabs(ctx, e.Layout, paneID, d)
	}
	return n.moveAcrossWorkspaces(ctx, e.Layout, paneID, d)
}

func (n *Nav) moveAcrossTabs(ctx context.Context, l api.PaneLayoutSnapshot, paneID string, d layout.Dir) error {
	tabs, err := n.tabs(ctx, l.WorkspaceID)
	if err != nil {
		return err
	}
	i := indexOfTab(tabs, l.TabID)
	if i < 0 {
		return fmt.Errorf("tab %s not found", l.TabID)
	}
	j := step(i, d)
	if j < 0 || j >= len(tabs) {
		// Past the end of the strip: a lone window is already at the end.
		if len(l.Panes) <= 1 {
			return nil
		}
		res, err := n.moveToNewTab(ctx, paneID, l.WorkspaceID, true)
		if err != nil {
			return err
		}
		if !layout.Forward(d) {
			return n.moveTab(ctx, res.Pane.TabID, 0)
		}
		return nil
	}
	target := tabs[j]
	tl, err := n.tabLayout(ctx, target.TabID)
	if err != nil {
		return err
	}
	anchor, ok := layout.EntryPane(tl, d, layout.RefFrac(l, paneID, d))
	if !ok {
		return nil
	}
	if _, err := n.moveToTab(ctx, paneID, target.TabID, anchor, api.SplitDirectionRight, 0.5); err != nil {
		return err
	}
	// Herdr places the moved pane right of the anchor. Travelling right we
	// enter at the left edge, so swap it to the anchor's left.
	if d == layout.Right {
		if err := n.swap(ctx, paneID, anchor); err != nil {
			return err
		}
	}
	if err := n.focusTab(ctx, target.TabID); err != nil {
		return err
	}
	return n.focusPane(ctx, paneID)
}

func (n *Nav) moveAcrossWorkspaces(ctx context.Context, l api.PaneLayoutSnapshot, paneID string, d layout.Dir) error {
	wss, err := n.workspaces(ctx)
	if err != nil {
		return err
	}
	i := indexOfWorkspace(wss, l.WorkspaceID)
	if i < 0 {
		return fmt.Errorf("workspace %s not found", l.WorkspaceID)
	}
	j := step(i, d)
	if j >= 0 && j < len(wss) {
		res, err := n.moveToTab(ctx, paneID, wss[j].ActiveTabID, "", api.SplitDirectionDown, 0.5)
		if err != nil {
			return err
		}
		return n.focusMoved(ctx, res)
	}
	// Past the end: open an empty slot and let the window replace its shell.
	if len(l.Panes) <= 1 && n.lastPaneOfWorkspace(ctx, l.WorkspaceID) {
		return nil // moving the only window of a workspace into a new one is a no-op
	}
	wsID, err := n.emptySlot(ctx, wss[i], d, false)
	if err != nil || wsID == "" {
		return err
	}
	delete(n.State.EmptySlots, wsID) // it receives a window: not empty
	ws, err := n.C.WorkspaceGet(ctx, api.WorkspaceTarget{WorkspaceID: wsID})
	if err != nil {
		return err
	}
	placeholder, err := n.tabTree(ctx, ws.Workspace.ActiveTabID)
	if err != nil {
		return err
	}
	res, err := n.moveToTab(ctx, paneID, ws.Workspace.ActiveTabID, "", api.SplitDirectionRight, 0.5)
	if err != nil {
		return err
	}
	for _, id := range layout.Leaves(placeholder) {
		if id != res.Pane.PaneID {
			_, _ = n.C.PaneClose(ctx, api.PaneTarget{PaneID: id})
		}
	}
	return n.focusMoved(ctx, res)
}

func (n *Nav) lastPaneOfWorkspace(ctx context.Context, wsID string) bool {
	ws, err := n.C.WorkspaceGet(ctx, api.WorkspaceTarget{WorkspaceID: wsID})
	return err == nil && ws.Workspace.PaneCount <= 1
}

// focusMoved focuses a pane after a cross-workspace move, which assigns it a
// new id.
func (n *Nav) focusMoved(ctx context.Context, res api.PaneMoveResult) error {
	if _, err := n.C.WorkspaceFocus(ctx, api.WorkspaceTarget{WorkspaceID: res.Pane.WorkspaceID}); err != nil {
		return err
	}
	if err := n.focusTab(ctx, res.Pane.TabID); err != nil {
		return err
	}
	return n.focusPane(ctx, res.Pane.PaneID)
}
