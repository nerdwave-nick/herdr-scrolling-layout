package nav

import (
	"context"

	"github.com/nerdwave-nick/herdr-go/api"
)

// NewWindow opens a new window next to the focused one, like niri opening a
// new column to the right. In a tab holding a lifted (maximized or
// fullscreen) group, it opens a new tab right after it instead, so the
// lifted view stays exclusive.
func (n *Nav) NewWindow(ctx context.Context) error {
	paneID, err := n.current(ctx)
	if err != nil {
		return err
	}
	p, err := n.C.PaneGet(ctx, api.PaneTarget{PaneID: paneID})
	if err != nil {
		return err
	}
	if _, lifted := n.State.Lifts[p.Pane.TabID]; lifted {
		return n.newTabAfter(ctx, p.Pane.WorkspaceID, p.Pane.TabID)
	}
	// The ratio is the share the split pane keeps; the new pane gets the rest.
	r, err := n.C.PaneSplit(ctx, api.PaneSplitParams{
		TargetPaneID: paneID,
		Direction:    api.SplitDirectionRight,
		Ratio:        1 - n.Cfg.NewWindowRatio,
		Focus:        true,
	})
	if err != nil {
		return err
	}
	return n.focusPane(ctx, r.Pane.PaneID)
}

func (n *Nav) newTabAfter(ctx context.Context, wsID, tabID string) error {
	tabs, err := n.tabs(ctx, wsID)
	if err != nil {
		return err
	}
	r, err := n.C.TabCreate(ctx, api.TabCreateParams{WorkspaceID: wsID, Focus: true})
	if err != nil {
		return err
	}
	if i := indexOfTab(tabs, tabID); i >= 0 {
		if err := n.moveTab(ctx, r.Tab.TabID, i+1); err != nil {
			return err
		}
	}
	return n.focusPane(ctx, r.RootPane.PaneID)
}
