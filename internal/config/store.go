// Role:    Clip + Token persistent storage (YAML, ~/.config/pinix/config.yaml)
// Depends: gopkg.in/yaml.v3, crypto/rand, sync
// Exports: Store, Config, ClipEntry, TokenEntry

package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// ClipEntry represents a registered clip with its working directory.
type ClipEntry struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	Workdir string `yaml:"workdir"`
}

// TokenEntry represents an access token bound to a clip (or super if ClipID is empty).
type TokenEntry struct {
	Token  string `yaml:"token"`
	ClipID string `yaml:"clip_id"` // empty = Super Token
	Label  string `yaml:"label"`
}

// Config is the top-level persistent configuration.
type Config struct {
	Clips  []ClipEntry  `yaml:"clips"`
	Tokens []TokenEntry `yaml:"tokens"`
}

// Store provides thread-safe access to Config with YAML persistence.
type Store struct {
	mu   sync.RWMutex
	cfg  *Config
	path string
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
		// file does not exist yet — use empty config
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
	s.cfg.Clips = append(s.cfg.Clips, entry)
	if err := s.save(); err != nil {
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

	s.cfg.Clips = append(s.cfg.Clips[:idx], s.cfg.Clips[idx+1:]...)
	return true, s.save()
}

// --- Token operations ---

// AddToken generates a 32-byte hex token. If clipID is empty it's a Super Token.
func (s *Store) AddToken(clipID, label string) (TokenEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, err := randomHex(32)
	if err != nil {
		return TokenEntry{}, err
	}

	entry := TokenEntry{Token: tok, ClipID: clipID, Label: label}
	s.cfg.Tokens = append(s.cfg.Tokens, entry)
	if err := s.save(); err != nil {
		return TokenEntry{}, err
	}
	return entry, nil
}

// GetToken looks up a token string. Returns the entry and true, or zero and false.
func (s *Store) GetToken(token string) (TokenEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, t := range s.cfg.Tokens {
		if t.Token == token {
			return t, true
		}
	}
	return TokenEntry{}, false
}

// RevokeToken removes a token. Returns false if not found.
func (s *Store) RevokeToken(token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, t := range s.cfg.Tokens {
		if t.Token == token {
			idx = i
			break
		}
	}
	if idx == -1 {
		return false, nil
	}

	s.cfg.Tokens = append(s.cfg.Tokens[:idx], s.cfg.Tokens[idx+1:]...)
	return true, s.save()
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
