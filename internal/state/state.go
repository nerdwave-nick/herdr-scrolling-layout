// Package state persists what the plugin needs to remember between key
// presses: where lifted (maximized / fullscreened) panes came from, and which
// workspaces it created as empty slots.
//
// Herdr ids are scoped to one server, so the file is keyed by socket path.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"

	"github.com/nerdwave-nick/herdr-scrolling-layout/internal/layout"
)

// Lift kinds.
const (
	KindColumn = "column"
	KindWindow = "window"
)

// Lift records a group of panes that was moved into its own tab.
type Lift struct {
	Kind        string           `json:"kind"`
	WorkspaceID string           `json:"workspace_id"`
	OriginTab   string           `json:"origin_tab"`
	Placement   layout.Placement `json:"placement"`
}

// State is the persisted plugin state.
type State struct {
	// Lifts maps the lifted tab id to its origin.
	Lifts map[string]Lift `json:"lifts"`
	// CreatedWorkspaces are empty-slot workspaces the plugin made.
	CreatedWorkspaces map[string]bool `json:"created_workspaces"`
}

// Store is a locked handle on the state file. Call Close when done.
type Store struct {
	State
	path string
	lock *os.File
}

// Memory returns an empty in-memory store, for tests.
func Memory() *Store {
	return &Store{State: State{Lifts: map[string]Lift{}, CreatedWorkspaces: map[string]bool{}}}
}

// Open locks and loads the state file for this Herdr server. Without
// HERDR_PLUGIN_STATE_DIR it returns an in-memory store.
func Open() (*Store, error) {
	s := &Store{}
	s.Lifts = map[string]Lift{}
	s.CreatedWorkspaces = map[string]bool{}
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return s, nil
	}
	sum := sha256.Sum256([]byte(os.Getenv("HERDR_SOCKET_PATH")))
	s.path = filepath.Join(dir, "state-"+hex.EncodeToString(sum[:6])+".json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	// Serialize rapid key presses: each action holds the lock while it acts.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, err
	}
	s.lock = lock
	b, err := os.ReadFile(s.path)
	if err == nil {
		_ = json.Unmarshal(b, &s.State)
	}
	if s.Lifts == nil {
		s.Lifts = map[string]Lift{}
	}
	if s.CreatedWorkspaces == nil {
		s.CreatedWorkspaces = map[string]bool{}
	}
	return s, nil
}

// Save writes the state atomically.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.State, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Close releases the lock.
func (s *Store) Close() {
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		s.lock.Close()
	}
}
