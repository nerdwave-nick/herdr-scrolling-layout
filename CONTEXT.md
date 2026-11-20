# Context

Domain vocabulary for herdr-scrolling-layout.

- **Strip**: the tabs of one workspace in display order, read left to right.
  Plays the role of niri's scrolling strip of columns.
- **Window**: a Herdr pane.
- **Column**: the panes sharing the focused pane's x position and width. Only
  meaningful when it is a subtree of the tab's layout tree.
- **Edge**: a pane touches the tab edge in a direction (`pane.edges`). Movement
  crosses into the adjacent tab or workspace only at an edge.
- **Entry pane**: the pane focused when entering a tab, on the edge facing the
  direction of travel, closest to the previous position along that edge.
- **Lift**: moving a group (column or window) into its own tab placed right
  after its origin tab. Maximize-column and fullscreen-window are lifts.
- **Placement**: what a lift records to undo itself: the parent split's
  direction and ratio, the group's side, and the sibling leaves.
- **Restore**: re-inserting a lifted tab's current tree into its origin tab's
  current tree next to the subtree covering the surviving sibling leaves.
- **Empty slot**: a workspace the plugin creates when moving past the first or
  last workspace; removed again when left untouched (one idle shell).
- **Arrange**: rebuilding a tab to match a layout tree using `pane.move` and
  `pane.swap`, which keep processes running.

Herdr facts this relies on:

- `tab.list` / `workspace.list` return display order; `number` is a creation
  ordinal, not a position.
- The socket answers one request per connection.
- Plugin actions are one-shot processes per key press; there is no
  supervised daemon for plugins.
