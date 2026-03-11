// Role:    Clip + Token persistent storage (YAML, ~/.config/pinix/config.yaml)
// Depends: gopkg.in/yaml.v3, crypto/rand, sync, time
// Exports: Store, Config, ClipEntry, TokenEntry

package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// MountEntry describes a host→container bind mount for a Clip's sandbox.
type MountEntry struct {
	Source   string `yaml:"source"` // host path
	Target   string `yaml:"target"` // path inside the box
	ReadOnly bool   `yaml:"read_only,omitempty"`
}

// ClipEntry represents a registered clip with its working directory.
type ClipEntry struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	Workdir string `yaml:"workdir"`
	// Mounts declares additional bind mounts for BoxLite sandbox execution.
	// The Workdir is always mounted to /clip automatically.
	Mounts []MountEntry `yaml:"mounts,omitempty"`
	// Image overrides the default OCI image for this Clip's sandbox.
	Image string `yaml:"image,omitempty"`
}

// TokenEntry represents a clip-scoped access token.
type TokenEntry struct {
	ID        string `yaml:"id"`
	Token     string `yaml:"token"`
	ClipID    string `yaml:"clip_id"`
	Label     string `yaml:"label"`
	CreatedAt string `yaml:"created_at"`
}

// Config is the top-level persistent configuration.
type Config struct {
	SuperToken string       `yaml:"super_token"`
	Clips      []ClipEntry  `yaml:"clips"`
	Tokens     []TokenEntry `yaml:"tokens"`
}

// Store provides thread-safe access to Config with YAML persistence.
type Store struct {
	mu   sync.RWMutex
	cfg  *Config
	path string
}

func (s *Store) mutate(fn func(cfg *Config) error) error {
	if err := fn(s.cfg); err != nil {
		return err
	}
	return s.save()
}

// DefaultPath returns ~/.config/pinix/config.yaml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "pinix", "config.yaml"), nil
}

// NewStore loads config from path (creates default if missing).
func NewStore(path string) (*Store, error) {
	cfg := &Config{}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	return &Store{cfg: cfg, path: path}, nil
}

func (s *Store) save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(s.cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(s.path, data, 0o600)
}

// --- Super Token ---

// GetSuperToken returns the static super token from config.
func (s *Store) GetSuperToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.SuperToken
}

// --- Clip operations ---

// AddClip generates an ID and persists a new clip.
func (s *Store) AddClip(name, workdir string) (ClipEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := randomHex(8)
	if err != nil {
		return ClipEntry{}, err
	}

	entry := ClipEntry{ID: id, Name: name, Workdir: workdir}
	if err := s.mutate(func(cfg *Config) error {
		cfg.Clips = append(cfg.Clips, entry)
		return nil
	}); err != nil {
		return ClipEntry{}, err
	}
	return entry, nil
}

// GetClips returns a copy of all clips.
func (s *Store) GetClips() []ClipEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ClipEntry, len(s.cfg.Clips))
	copy(out, s.cfg.Clips)
	return out
}

// GetClip returns the clip with the given ID, or false.
func (s *Store) GetClip(id string) (ClipEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, c := range s.cfg.Clips {
		if c.ID == id {
			return c, true
		}
	}
	return ClipEntry{}, false
}

// GetClipByName returns the clip with the given name, or false.
func (s *Store) GetClipByName(name string) (ClipEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, c := range s.cfg.Clips {
		if c.Name == name {
			return c, true
		}
	}
	return ClipEntry{}, false
}

// DeleteClip removes a clip by ID. Returns false if not found.
func (s *Store) DeleteClip(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, c := range s.cfg.Clips {
		if c.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return false, nil
	}

	if err := s.mutate(func(cfg *Config) error {
		cfg.Clips = append(cfg.Clips[:idx], cfg.Clips[idx+1:]...)
		return nil
	}); err != nil {
		return false, err
	}
	return true, nil
}

// --- Token operations ---

// AddToken generates a clip-scoped token with a unique ID.
func (s *Store) AddToken(clipID, label string) (TokenEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, err := randomHex(32)
	if err != nil {
		return TokenEntry{}, err
	}

	idHex, err := randomHex(6)
	if err != nil {
		return TokenEntry{}, err
	}

	entry := TokenEntry{
		ID:        "t_" + idHex,
		Token:     tok,
		ClipID:    clipID,
		Label:     label,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	if err := s.mutate(func(cfg *Config) error {
		cfg.Tokens = append(cfg.Tokens, entry)
		return nil
	}); err != nil {
		return TokenEntry{}, err
	}
	return entry, nil
}

// LookupToken finds a token by its value. Used for auth.
func (s *Store) LookupToken(token string) (TokenEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, t := range s.cfg.Tokens {
		if t.Token == token {
			return t, true
		}
	}
	return TokenEntry{}, false
}

// GetTokens returns a copy of all tokens.
func (s *Store) GetTokens() []TokenEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]TokenEntry, len(s.cfg.Tokens))
	copy(out, s.cfg.Tokens)
	return out
}

// RevokeTokenByID removes a token by its ID. Returns false if not found.
func (s *Store) RevokeTokenByID(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, t := range s.cfg.Tokens {
		if t.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return false, nil
	}

	if err := s.mutate(func(cfg *Config) error {
		cfg.Tokens = append(cfg.Tokens[:idx], cfg.Tokens[idx+1:]...)
		return nil
	}); err != nil {
		return false, err
	}
	return true, nil
}

// RevokeTokensByClipID removes all tokens associated with a clip.
// Returns the number of tokens revoked.
func (s *Store) RevokeTokensByClipID(clipID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var kept []TokenEntry
	count := 0
	for _, t := range s.cfg.Tokens {
		if t.ClipID == clipID {
			count++
		} else {
			kept = append(kept, t)
		}
	}
	if count == 0 {
		return 0, nil
	}
	if err := s.mutate(func(cfg *Config) error {
		cfg.Tokens = kept
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
