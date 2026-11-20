// Package config reads the plugin's config.toml from HERDR_PLUGIN_CONFIG_DIR.
//
// Only a flat subset of TOML is understood: `key = true|false` lines, with
// `#` comments and blank lines. That is all the plugin needs, and it keeps the
// module dependency-free.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds plugin settings.
type Config struct {
	// EmptyWorkspaceBelow: focusing down past the last workspace creates a
	// fresh workspace, like niri's trailing empty workspace.
	EmptyWorkspaceBelow bool
	// EmptyWorkspaceAbove: same for focusing up past the first workspace
	// (niri's empty-workspace-above-first).
	EmptyWorkspaceAbove bool
	// CleanupEmptyWorkspaces: closing a workspace this plugin created when
	// you leave it while it still only holds one idle shell.
	CleanupEmptyWorkspaces bool
	// CrossWorkspaces: up/down at a tab's edge continues into the adjacent
	// workspace. When false, up/down stays inside the tab.
	CrossWorkspaces bool
	// CrossTabs: left/right at a tab's edge continues into the adjacent tab.
	CrossTabs bool
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		EmptyWorkspaceBelow:    true,
		EmptyWorkspaceAbove:    false,
		CleanupEmptyWorkspaces: true,
		CrossWorkspaces:        true,
		CrossTabs:              true,
	}
}

// Load reads config.toml from the plugin config dir. A missing file yields
// the defaults.
func Load() (Config, error) {
	cfg := Default()
	dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if dir == "" {
		return cfg, nil
	}
	path := filepath.Join(dir, "config.toml")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	if err := parse(f, path, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func parse(f *os.File, path string, cfg *Config) error {
	fields := map[string]*bool{
		"empty_workspace_below":    &cfg.EmptyWorkspaceBelow,
		"empty_workspace_above":    &cfg.EmptyWorkspaceAbove,
		"cleanup_empty_workspaces": &cfg.CleanupEmptyWorkspaces,
		"cross_workspaces":         &cfg.CrossWorkspaces,
		"cross_tabs":               &cfg.CrossTabs,
	}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected key = value", path, n)
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		dst, known := fields[key]
		if !known {
			return fmt.Errorf("%s:%d: unknown key %q", path, n, key)
		}
		switch val {
		case "true":
			*dst = true
		case "false":
			*dst = false
		default:
			return fmt.Errorf("%s:%d: %s must be true or false", path, n, key)
		}
	}
	return sc.Err()
}
