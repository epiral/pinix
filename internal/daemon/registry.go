// Role:    File-locked config registry for Pinix daemon state
// Depends: encoding/json, fmt, os, path/filepath, sort, strings, syscall
// Exports: ClipConfig, ManifestCache, Config, Registry, DefaultRootDir, DefaultConfigPath, DefaultSocketPath, DefaultClipsDir, NewRegistry

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type ClipConfig struct {
	Name     string         `json:"name"`
	Source   string         `json:"source"`
	Path     string         `json:"path"`
	Token    string         `json:"token,omitempty"`
	Manifest *ManifestCache `json:"manifest,omitempty"`
}

type ManifestCache struct {
	Name     string   `json:"name"`
	Domain   string   `json:"domain"`
	Commands []string `json:"commands"`
}

type Config struct {
	SuperToken string                `json:"super_token,omitempty"`
	Clips      map[string]ClipConfig `json:"clips"`
}

type Registry struct {
	path     string
	lockPath string
}

func DefaultRootDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".pinix"), nil
}

func DefaultConfigPath() (string, error) {
	root, err := DefaultRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.json"), nil
}

func DefaultSocketPath() (string, error) {
	root, err := DefaultRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "pinix.sock"), nil
}

func DefaultClipsDir() (string, error) {
	root, err := DefaultRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "clips"), nil
}

func NewRegistry(path string) (*Registry, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}

	registry := &Registry{path: path, lockPath: path + ".lock"}
	if err := registry.ensureBaseDir(); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *Registry) Path() string {
	return r.path
}

func (r *Registry) RootDir() string {
	return filepath.Dir(r.path)
}

func (r *Registry) ClipsDir() string {
	return filepath.Join(r.RootDir(), "clips")
}

func (r *Registry) GetSuperToken() (string, error) {
	var token string
	err := r.withConfig(false, func(cfg *Config) error {
		token = cfg.SuperToken
		return nil
	})
	return token, err
}

func (r *Registry) SetSuperToken(token string) error {
	return r.withConfig(true, func(cfg *Config) error {
		cfg.SuperToken = token
		return nil
	})
}

func (r *Registry) ListClips() ([]ClipConfig, error) {
	var clips []ClipConfig
	err := r.withConfig(false, func(cfg *Config) error {
		clips = make([]ClipConfig, 0, len(cfg.Clips))
		for _, clip := range cfg.Clips {
			clips = append(clips, clip)
		}
		sort.Slice(clips, func(i, j int) bool {
			return clips[i].Name < clips[j].Name
		})
		return nil
	})
	return clips, err
}

func (r *Registry) GetClip(name string) (ClipConfig, bool, error) {
	var (
		clip ClipConfig
		ok   bool
	)
	err := r.withConfig(false, func(cfg *Config) error {
		clip, ok = cfg.Clips[name]
		return nil
	})
	return clip, ok, err
}

func (r *Registry) PutClip(clip ClipConfig) error {
	if strings.TrimSpace(clip.Name) == "" {
		return fmt.Errorf("clip name is required")
	}
	return r.withConfig(true, func(cfg *Config) error {
		cfg.Clips[clip.Name] = clip
		return nil
	})
}

func (r *Registry) RemoveClip(name string) (ClipConfig, bool, error) {
	var (
		removed ClipConfig
		ok      bool
	)
	err := r.withConfig(true, func(cfg *Config) error {
		removed, ok = cfg.Clips[name]
		if ok {
			delete(cfg.Clips, name)
		}
		return nil
	})
	return removed, ok, err
}

func (r *Registry) withConfig(exclusive bool, fn func(cfg *Config) error) error {
	if err := r.ensureBaseDir(); err != nil {
		return err
	}

	lockFile, err := os.OpenFile(r.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open config lock: %w", err)
	}
	defer lockFile.Close()

	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(lockFile.Fd()), mode); err != nil {
		return fmt.Errorf("lock config: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	cfg, err := r.loadLocked()
	if err != nil {
		return err
	}
	if err := fn(cfg); err != nil {
		return err
	}
	if !exclusive {
		return nil
	}
	return r.saveLocked(cfg)
}

func (r *Registry) ensureBaseDir() error {
	if err := os.MkdirAll(r.RootDir(), 0o755); err != nil {
		return fmt.Errorf("create pinix dir: %w", err)
	}
	if err := os.MkdirAll(r.ClipsDir(), 0o755); err != nil {
		return fmt.Errorf("create clips dir: %w", err)
	}
	return nil
}

func (r *Registry) loadLocked() (*Config, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Clips: make(map[string]ClipConfig)}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &Config{Clips: make(map[string]ClipConfig)}, nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Clips == nil {
		cfg.Clips = make(map[string]ClipConfig)
	}
	return &cfg, nil
}

func (r *Registry) saveLocked(cfg *Config) error {
	if cfg.Clips == nil {
		cfg.Clips = make(map[string]ClipConfig)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')

	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write config temp file: %w", err)
	}
	if err := os.Rename(tmpPath, r.path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
