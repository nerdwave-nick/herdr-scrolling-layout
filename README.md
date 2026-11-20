# herdr-scrolling-layout

niri-style keyboard navigation for [Herdr](https://herdr.dev).

| niri | Herdr |
|---|---|
| workspaces (vertical) | workspaces |
| the scrolling strip of columns | the tabs of a workspace, left to right |
| windows | panes inside a tab |
| maximize column / fullscreen window | the column / pane lifted into its own tab |

- **focus left/right** moves between panes and, at the tab edge, continues into
  the previous/next tab, landing on the pane at the same height.
- **focus up/down** moves between panes and, at the edge, continues into the
  previous/next workspace (niri's `focus-window-or-workspace-down`). Past the
  last workspace an empty one is created; leaving it untouched removes it again.
- **move left/right/up/down** swaps the pane inside its tab and carries it into
  the neighbouring tab or workspace at the edge.
- **maximize-column** lifts the focused column into its own tab right after the
  current one (a 2x2 becomes full-width 1-over-1). Toggle again to put it back.
- **fullscreen-window** does the same for the focused pane alone.
- **new-window** opens a pane to the right of the focused one (50% of it by
  default). In a maximized/fullscreen tab it opens a new tab right after it.

Restores are exact: the lifted tree is re-inserted at its recorded split with
the original ratio, and processes keep running (`pane.move` never restarts
them). If the layout changed meanwhile, the group goes next to whatever now
covers its former neighbours.

## Install

Requires Go 1.24+ and Herdr 0.9.5+.

```sh
herdr plugin install nerdwave-nick/herdr-scrolling-layout
```

Or from a local checkout:

```sh
go build -o bin/herdr-scrolling-layout .
herdr plugin link "$PWD"
```

Then add keybindings. The plugin ships no keys of its own; generate them for
the modifier you like and paste into `~/.config/herdr/config.toml` (for a
GitHub install, run the binary under the `plugin_root` shown by
`herdr plugin list`):

```sh
./bin/herdr-scrolling-layout keys -mod alt          # alt+h/j/k/l, alt+shift+h/j/k/l, alt+f, alt+shift+f
./bin/herdr-scrolling-layout keys -mod ctrl+alt
./bin/herdr-scrolling-layout keys -mod prefix -keys left,down,up,right
herdr server reload-config
```

## Configuration

Optional `config.toml` in `herdr plugin config-dir herdr-scrolling-layout`:

```toml
empty_workspace_below = true     # down past the last workspace creates one
empty_workspace_above = false    # niri's empty-workspace-above-first
cleanup_empty_workspaces = true  # remove a created workspace you leave untouched
cross_tabs = true                # left/right continues into adjacent tabs
cross_workspaces = true          # up/down continues into adjacent workspaces
new_window_ratio = 0.5           # share of the focused pane a new window takes
```

## Development

The Herdr client lives in [herdr-go](https://github.com/nerdwave-nick/herdr-go).
To develop both together, check herdr-go out next to this repo and add a
(gitignored) workspace: `go work init . ../herdr-go`.

```sh
go test ./...        # unit tests + live tests against throwaway herdr servers
go test -short ./... # unit tests only
```

Live tests start an isolated headless `herdr server` per test (own XDG dirs),
so they never touch your sessions.

## Limitations

- Platforms: Linux and macOS (state locking uses `flock`).
- A lifted tab is tracked by id in plugin state; tabs moved to another
  workspace are not restored.
- If a column is not a subtree of the layout (rows were split first),
  maximize-column lifts just the focused pane.
