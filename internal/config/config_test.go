package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, body string) (Config, error) {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	return Load()
}

func TestMissingFileGivesDefaults(t *testing.T) {
	c, err := load(t, "")
	if err != nil || c != Default() {
		t.Fatalf("got %+v, %v", c, err)
	}
}

func TestParse(t *testing.T) {
	c, err := load(t, `
# niri: empty-workspace-above-first
empty_workspace_above = true   # trailing comment
cross_tabs=false
`)
	if err != nil {
		t.Fatal(err)
	}
	if !c.EmptyWorkspaceAbove || c.CrossTabs || !c.EmptyWorkspaceBelow {
		t.Fatalf("got %+v", c)
	}
}

func TestErrors(t *testing.T) {
	for _, body := range []string{"nope = true", "cross_tabs = yes", "cross_tabs"} {
		if _, err := load(t, body); err == nil || !strings.Contains(err.Error(), "config.toml:1") {
			t.Errorf("%q: want line-numbered error, got %v", body, err)
		}
	}
}
