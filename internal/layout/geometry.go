// Package layout holds pure geometry and layout-tree helpers. Nothing here
// talks to Herdr, so it is unit-tested directly.
package layout

import (
	"math"
	"sort"

	"github.com/nerdwave-nick/herdr-go/api"
)

// Dir is a direction of travel.
type Dir = api.PaneDirection

const (
	Left  = api.PaneDirectionLeft
	Right = api.PaneDirectionRight
	Up    = api.PaneDirectionUp
	Down  = api.PaneDirectionDown
)

// ParseDir validates a direction string.
func ParseDir(s string) (Dir, bool) {
	switch d := Dir(s); d {
	case Left, Right, Up, Down:
		return d, true
	}
	return "", false
}

// Horizontal reports whether d is left or right.
func Horizontal(d Dir) bool { return d == Left || d == Right }

// Forward reports whether d moves toward higher indices (right or down).
func Forward(d Dir) bool { return d == Right || d == Down }

// AtEdge reports whether the edges result says the pane touches the tab
// edge in direction d.
func AtEdge(e api.PaneEdgesResult, d Dir) bool {
	switch d {
	case Left:
		return e.Left
	case Right:
		return e.Right
	case Up:
		return e.Up
	default:
		return e.Down
	}
}

// Find returns the layout entry for pane id.
func Find(l api.PaneLayoutSnapshot, id string) (api.PaneLayoutPane, bool) {
	for _, p := range l.Panes {
		if p.PaneID == id {
			return p, true
		}
	}
	return api.PaneLayoutPane{}, false
}

func right(r api.PaneLayoutRect) int       { return r.X + r.Width }
func bottom(r api.PaneLayoutRect) int      { return r.Y + r.Height }
func centerX(r api.PaneLayoutRect) float64 { return float64(r.X) + float64(r.Width)/2 }
func centerY(r api.PaneLayoutRect) float64 { return float64(r.Y) + float64(r.Height)/2 }

// EntryPane picks the pane to focus when entering a tab while travelling in
// direction d: moving right enters at the tab's left edge, and so on. Among
// the edge panes it prefers the one whose span contains the reference point
// (refFrac, a 0..1 fraction of the area along the edge), so the vertical
// position survives a horizontal move, as in niri.
func EntryPane(l api.PaneLayoutSnapshot, d Dir, refFrac float64) (string, bool) {
	if len(l.Panes) == 0 {
		return "", false
	}
	if l.Zoomed && l.FocusedPaneID != "" {
		return l.FocusedPaneID, true
	}
	var edge func(api.PaneLayoutRect) int
	var better func(a, b int) bool
	switch d {
	case Right: // enter at the left edge
		edge, better = func(r api.PaneLayoutRect) int { return r.X }, func(a, b int) bool { return a < b }
	case Left: // enter at the right edge
		edge, better = right, func(a, b int) bool { return a > b }
	case Down: // enter at the top edge
		edge, better = func(r api.PaneLayoutRect) int { return r.Y }, func(a, b int) bool { return a < b }
	default: // Up: enter at the bottom edge
		edge, better = bottom, func(a, b int) bool { return a > b }
	}
	best := edge(l.Panes[0].Rect)
	for _, p := range l.Panes[1:] {
		if v := edge(p.Rect); better(v, best) {
			best = v
		}
	}
	var cands []api.PaneLayoutPane
	for _, p := range l.Panes {
		if edge(p.Rect) == best {
			cands = append(cands, p)
		}
	}
	var ref float64
	var span func(api.PaneLayoutRect) [2]float64
	if Horizontal(d) {
		ref = float64(l.Area.Y) + refFrac*float64(l.Area.Height)
		span = func(r api.PaneLayoutRect) [2]float64 { return [2]float64{float64(r.Y), float64(bottom(r))} }
	} else {
		ref = float64(l.Area.X) + refFrac*float64(l.Area.Width)
		span = func(r api.PaneLayoutRect) [2]float64 { return [2]float64{float64(r.X), float64(right(r))} }
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return spanDist(span(cands[i].Rect), ref) < spanDist(span(cands[j].Rect), ref)
	})
	return cands[0].PaneID, true
}

func spanDist(s [2]float64, ref float64) float64 {
	lo, hi := s[0], s[1]
	if ref >= lo && ref < hi {
		return 0
	}
	return math.Min(math.Abs(ref-lo), math.Abs(ref-hi))
}

// RefFrac returns the pane's center along the axis perpendicular to d, as a
// fraction of the tab area. It carries "where on screen am I" across tabs.
func RefFrac(l api.PaneLayoutSnapshot, paneID string, d Dir) float64 {
	p, ok := Find(l, paneID)
	if !ok {
		return 0.5
	}
	if Horizontal(d) {
		if l.Area.Height == 0 {
			return 0.5
		}
		return (centerY(p.Rect) - float64(l.Area.Y)) / float64(l.Area.Height)
	}
	if l.Area.Width == 0 {
		return 0.5
	}
	return (centerX(p.Rect) - float64(l.Area.X)) / float64(l.Area.Width)
}

// Column returns the panes stacked in the same column as paneID (same x and
// width), top to bottom.
func Column(l api.PaneLayoutSnapshot, paneID string) []api.PaneLayoutPane {
	me, ok := Find(l, paneID)
	if !ok {
		return nil
	}
	var col []api.PaneLayoutPane
	for _, p := range l.Panes {
		if p.Rect.X == me.Rect.X && p.Rect.Width == me.Rect.Width {
			col = append(col, p)
		}
	}
	sort.SliceStable(col, func(i, j int) bool { return col[i].Rect.Y < col[j].Rect.Y })
	return col
}

// StackRatios returns the split ratios that rebuild a vertical stack with
// the given heights by repeatedly splitting the last pane downward: ratio i
// is the share pane i keeps of the space left for panes i..n-1.
func StackRatios(heights []int) []float64 {
	n := len(heights)
	if n < 2 {
		return nil
	}
	ratios := make([]float64, n-1)
	for i := 0; i < n-1; i++ {
		rest := 0
		for _, h := range heights[i:] {
			rest += h
		}
		if rest <= 0 {
			ratios[i] = 1 / float64(n-i)
			continue
		}
		ratios[i] = clampRatio(float64(heights[i]) / float64(rest))
	}
	return ratios
}

func clampRatio(r float64) float64 { return math.Max(0.1, math.Min(0.9, r)) }
