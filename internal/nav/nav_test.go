package nav_test

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	herdr "github.com/nerdwave-nick/herdr-go"
	"github.com/nerdwave-nick/herdr-go/api"
	"github.com/nerdwave-nick/herdr-go/herdrtest"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/config"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/layout"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/nav"
	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/state"
)

type fixture struct {
	t   *testing.T
	ctx context.Context
	c   *herdr.Client
	n   *nav.Nav
}

func setup(t *testing.T) *fixture {
	t.Helper()
	c := herdrtest.Server(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return &fixture{t: t, ctx: ctx, c: c, n: &nav.Nav{C: c, Cfg: config.Default(), State: state.Memory()}}
}

func (f *fixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

// workspace creates a focused workspace and returns its id, first tab and pane.
func (f *fixture) workspace() (ws, tab, pane string) {
	f.t.Helper()
	r, err := f.c.WorkspaceCreate(f.ctx, api.WorkspaceCreateParams{Focus: true})
	f.must(err)
	return r.Workspace.WorkspaceID, r.Tab.TabID, r.RootPane.PaneID
}

func (f *fixture) split(target string, d api.SplitDirection) string {
	f.t.Helper()
	r, err := f.c.PaneSplit(f.ctx, api.PaneSplitParams{TargetPaneID: target, Direction: d})
	f.must(err)
	return r.Pane.PaneID
}

// grid turns pane p1 into a column-first 2x2: p1 p3 / p2 p4.
func (f *fixture) grid(p1 string) (p2, p3, p4 string) {
	p3 = f.split(p1, api.SplitDirectionRight)
	p2 = f.split(p1, api.SplitDirectionDown)
	p4 = f.split(p3, api.SplitDirectionDown)
	return
}

func (f *fixture) tab(ws string) (tab, pane string) {
	f.t.Helper()
	r, err := f.c.TabCreate(f.ctx, api.TabCreateParams{WorkspaceID: ws})
	f.must(err)
	return r.Tab.TabID, r.RootPane.PaneID
}

func (f *fixture) focus(pane string) {
	f.t.Helper()
	p, err := f.c.PaneGet(f.ctx, api.PaneTarget{PaneID: pane})
	f.must(err)
	_, err = f.c.WorkspaceFocus(f.ctx, api.WorkspaceTarget{WorkspaceID: p.Pane.WorkspaceID})
	f.must(err)
	_, err = f.c.TabFocus(f.ctx, api.TabTarget{TabID: p.Pane.TabID})
	f.must(err)
	_, err = f.c.PaneFocus(f.ctx, api.PaneTarget{PaneID: pane})
	f.must(err)
}

func (f *fixture) focused() api.PaneInfo {
	f.t.Helper()
	r, err := f.c.PaneCurrent(f.ctx, api.PaneCurrentParams{})
	f.must(err)
	return r.Pane
}

func (f *fixture) wantFocus(pane string) {
	f.t.Helper()
	if got := f.focused().PaneID; got != pane {
		f.t.Fatalf("focused %s, want %s", got, pane)
	}
}

func (f *fixture) leaves(tab string) []string {
	f.t.Helper()
	r, err := f.c.LayoutExport(f.ctx, api.LayoutExportParams{TabID: tab})
	f.must(err)
	return layout.Leaves(r.Layout.Root)
}

func (f *fixture) tabIDs(ws string) []string {
	f.t.Helper()
	r, err := f.c.TabList(f.ctx, api.TabListParams{WorkspaceID: ws})
	f.must(err)
	var ids []string
	for _, t := range r.Tabs {
		ids = append(ids, t.TabID)
	}
	return ids
}

func (f *fixture) workspaceIDs() []string {
	f.t.Helper()
	r, err := f.c.WorkspaceList(f.ctx)
	f.must(err)
	var ids []string
	for _, w := range r.Workspaces {
		ids = append(ids, w.WorkspaceID)
	}
	return ids
}

func (f *fixture) rects(pane string) map[string]api.PaneLayoutRect {
	f.t.Helper()
	r, err := f.c.PaneLayout(f.ctx, api.PaneLayoutParams{PaneID: pane})
	f.must(err)
	m := map[string]api.PaneLayoutRect{}
	for _, p := range r.Layout.Panes {
		m[p.PaneID] = p.Rect
	}
	return m
}

// waitShellIdle waits until the pane's shell is in the foreground, so the
// empty-workspace cleanup sees it as pristine.
func (f *fixture) waitShellIdle(pane string) {
	f.t.Helper()
	for i := 0; i < 100; i++ {
		r, err := f.c.PaneProcessInfo(f.ctx, api.PaneProcessInfoParams{PaneID: pane})
		if err == nil && r.ProcessInfo.ShellPID != 0 && r.ProcessInfo.ForegroundProcessGroupID == r.ProcessInfo.ShellPID {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.t.Fatalf("shell in %s never became idle", pane)
}

// ---- tests ----------------------------------------------------------------

func TestFocusWithinAndAcrossTabs(t *testing.T) {
	f := setup(t)
	ws, _, p1 := f.workspace()
	_, p3, p4 := f.grid(p1)
	_, q1 := f.tab(ws)
	q2 := f.split(q1, api.SplitDirectionRight)

	f.focus(p4)
	f.must(f.n.Focus(f.ctx, layout.Up)) // inside the tab
	f.wantFocus(p3)

	f.focus(p4)
	f.must(f.n.Focus(f.ctx, layout.Right)) // right edge: next tab, left edge
	f.wantFocus(q1)

	f.must(f.n.Focus(f.ctx, layout.Right)) // inside tab 2
	f.wantFocus(q2)
	f.must(f.n.Focus(f.ctx, layout.Right)) // end of the strip: stay
	f.wantFocus(q2)

	f.focus(q1)
	f.must(f.n.Focus(f.ctx, layout.Left)) // back: right edge of tab 1, vertical middle
	if got := f.focused().PaneID; got != p3 && got != p4 {
		t.Fatalf("focused %s, want the right column", got)
	}
}

func TestFocusAcrossWorkspacesWithEmptySlot(t *testing.T) {
	f := setup(t)
	w1, _, a := f.workspace()
	w2, _, b := f.workspace()
	f.focus(a)

	f.must(f.n.Focus(f.ctx, layout.Down))
	f.wantFocus(b)

	f.must(f.n.Focus(f.ctx, layout.Down)) // past the last: empty slot
	ids := f.workspaceIDs()
	if len(ids) != 3 || ids[0] != w1 || ids[1] != w2 {
		t.Fatalf("workspaces %v", ids)
	}
	slot := ids[2]
	cur := f.focused()
	if cur.WorkspaceID != slot {
		t.Fatalf("focused workspace %s, want new slot %s", cur.WorkspaceID, slot)
	}
	f.must(f.n.Focus(f.ctx, layout.Down)) // already in the empty slot: no second one
	if n := len(f.workspaceIDs()); n != 3 {
		t.Fatalf("%d workspaces after second press", n)
	}

	f.waitShellIdle(cur.PaneID)
	f.must(f.n.Focus(f.ctx, layout.Up)) // leaving the untouched slot closes it
	f.wantFocus(b)
	if ids := f.workspaceIDs(); len(ids) != 2 {
		t.Fatalf("empty slot not cleaned up: %v", ids)
	}

	f.focus(a)
	f.must(f.n.Focus(f.ctx, layout.Up)) // above the first: disabled by default
	f.wantFocus(a)
	f.n.Cfg.EmptyWorkspaceAbove = true
	f.must(f.n.Focus(f.ctx, layout.Up))
	if ids := f.workspaceIDs(); len(ids) != 3 || ids[1] != w1 {
		t.Fatalf("slot above first not created at the top: %v", ids)
	}
}

func TestMoveAcrossTabs(t *testing.T) {
	f := setup(t)
	ws, t1, a := f.workspace()
	b := f.split(a, api.SplitDirectionRight)
	t2, c := f.tab(ws)

	f.focus(b)
	f.must(f.n.Move(f.ctx, layout.Right))
	if got := f.leaves(t2); !slices.Equal(got, []string{b, c}) {
		t.Fatalf("tab 2 = %v, want [%s %s]", got, b, c)
	}
	if got := f.leaves(t1); !slices.Equal(got, []string{a}) {
		t.Fatalf("tab 1 = %v", got)
	}
	f.wantFocus(b)

	f.must(f.n.Move(f.ctx, layout.Left)) // back into tab 1, at its right edge
	if got := f.leaves(t1); !slices.Equal(got, []string{a, b}) {
		t.Fatalf("tab 1 = %v, want [%s %s]", got, a, b)
	}

	f.focus(c)
	f.must(f.n.Move(f.ctx, layout.Right)) // lone window at the end of the strip: no-op
	if got := f.tabIDs(ws); len(got) != 2 {
		t.Fatalf("tabs %v", got)
	}
}

func TestMoveAcrossWorkspaces(t *testing.T) {
	f := setup(t)
	w1, _, a := f.workspace()
	b := f.split(a, api.SplitDirectionRight)
	w2, t2, c := f.workspace()

	f.focus(b)
	f.must(f.n.Move(f.ctx, layout.Down))
	got := f.leaves(t2)
	if len(got) != 2 || got[0] != c {
		t.Fatalf("workspace 2 = %v", got)
	}
	if cur := f.focused(); cur.WorkspaceID != w2 || cur.PaneID != got[1] {
		t.Fatalf("focus %+v, want moved pane %s in %s", cur, got[1], w2)
	}
	_ = w1
}

func near(a, b int) bool { return math.Abs(float64(a-b)) <= 1 }

func sameRects(t *testing.T, got, want map[string]api.PaneLayoutRect) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d panes, want %d", len(got), len(want))
	}
	for id, w := range want {
		g, ok := got[id]
		if !ok || !near(g.X, w.X) || !near(g.Y, w.Y) || !near(g.Width, w.Width) || !near(g.Height, w.Height) {
			t.Fatalf("pane %s at %+v, want %+v", id, g, w)
		}
	}
}

func TestMaximizeColumnRoundTrip(t *testing.T) {
	f := setup(t)
	ws, t1, p1 := f.workspace()
	p2, p3, p4 := f.grid(p1)
	before := f.rects(p1)

	f.focus(p3)
	f.must(f.n.MaximizeColumn(f.ctx))
	tabs := f.tabIDs(ws)
	if len(tabs) != 2 || tabs[0] != t1 {
		t.Fatalf("tabs %v", tabs)
	}
	if got := f.leaves(tabs[1]); !slices.Equal(got, []string{p3, p4}) {
		t.Fatalf("lifted tab = %v", got)
	}
	if got := f.leaves(t1); !slices.Equal(got, []string{p1, p2}) {
		t.Fatalf("origin tab = %v", got)
	}
	f.wantFocus(p3)
	r := f.rects(p3)
	if r[p3].Width != r[p4].Width || r[p3].X != r[p4].X {
		t.Fatalf("lifted column not stacked full width: %+v", r)
	}

	f.must(f.n.MaximizeColumn(f.ctx)) // toggle back
	if got := f.tabIDs(ws); len(got) != 1 {
		t.Fatalf("lifted tab not closed: %v", got)
	}
	sameRects(t, f.rects(p1), before)
	f.wantFocus(p3)
}

func TestFullscreenWindowRoundTrip(t *testing.T) {
	f := setup(t)
	ws, t1, p1 := f.workspace()
	p2, p3, p4 := f.grid(p1)
	before := f.rects(p1)

	f.focus(p2)
	f.must(f.n.FullscreenWindow(f.ctx))
	tabs := f.tabIDs(ws)
	if len(tabs) != 2 || !slices.Equal(f.leaves(tabs[1]), []string{p2}) {
		t.Fatalf("tabs %v", tabs)
	}
	if got := f.leaves(t1); !slices.Equal(got, []string{p1, p3, p4}) {
		t.Fatalf("origin tab = %v", got)
	}

	f.must(f.n.FullscreenWindow(f.ctx))
	sameRects(t, f.rects(p1), before)
	f.wantFocus(p2)

	// A window that was first in its split goes back before its sibling.
	f.focus(p3)
	f.must(f.n.FullscreenWindow(f.ctx))
	f.must(f.n.FullscreenWindow(f.ctx))
	sameRects(t, f.rects(p1), before)
}

func TestLoneWindowDoesNotLift(t *testing.T) {
	f := setup(t)
	ws, _, _ := f.workspace()
	f.must(f.n.FullscreenWindow(f.ctx))
	f.must(f.n.MaximizeColumn(f.ctx))
	if got := f.tabIDs(ws); len(got) != 1 {
		t.Fatalf("tabs %v", got)
	}
}

func TestRoundTripUnevenRatios(t *testing.T) {
	f := setup(t)
	_, _, p1 := f.workspace()
	p2, p3, p4 := f.grid(p1)
	_, err := f.c.PaneResize(f.ctx, api.PaneResizeParams{PaneID: p1, Direction: layout.Right, Amount: 0.2})
	f.must(err)
	_, err = f.c.PaneResize(f.ctx, api.PaneResizeParams{PaneID: p3, Direction: layout.Down, Amount: 0.25})
	f.must(err)
	before := f.rects(p1)
	if before[p1].Width == before[p3].Width || before[p3].Height == before[p4].Height {
		t.Fatalf("resize had no effect: %+v", before)
	}

	for _, tc := range []struct {
		focus  string
		toggle func(context.Context) error
	}{
		{p3, f.n.MaximizeColumn}, {p4, f.n.MaximizeColumn},
		{p1, f.n.MaximizeColumn}, {p2, f.n.FullscreenWindow},
		{p3, f.n.FullscreenWindow}, {p4, f.n.FullscreenWindow},
	} {
		f.focus(tc.focus)
		f.must(tc.toggle(f.ctx))
		f.must(tc.toggle(f.ctx))
		sameRects(t, f.rects(p1), before)
		f.wantFocus(tc.focus)
	}
}
