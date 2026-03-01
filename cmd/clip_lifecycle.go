// Role:    `pinix clip install|upgrade|uninstall` — 本地 Clip 生命周期管理
// Depends: archive/zip, config.Store, cobra
// Exports: (registered via init)

package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/epiral/pinix/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// clipMeta represents metadata from clip.yaml inside a .clip package.
type clipMeta struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

// codeLayerEntries are the directories/files replaced during upgrade.
var codeLayerEntries = []string{"clip.yaml", "commands", "bin", "lib", "web"}

var clipInstallName string

var clipInstallCmd = &cobra.Command{
	Use:   "install <file.clip>",
	Short: "Install a clip from a .clip package",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		clipFile := args[0]

		// 打开 zip 包
		r, err := zip.OpenReader(clipFile)
		if err != nil {
			return fmt.Errorf("open clip package: %w", err)
		}
		defer r.Close()

		// 读取 clip.yaml
		meta, err := readClipMetaFromZip(r.File)
		if err != nil {
			return err
		}

		// 确定实例名
		name := meta.Name
		if clipInstallName != "" {
			name = clipInstallName
		}

		// 确定 clips 目录
		clipsDir, err := defaultClipsDir()
		if err != nil {
			return err
		}
		instanceDir := filepath.Join(clipsDir, name)

		// 检查是否已存在（目录或 config 注册）
		if _, err := os.Stat(instanceDir); err == nil {
			return fmt.Errorf("instance %q already exists at %s (use 'clip upgrade' instead)", name, instanceDir)
		}
		{
			store, err := openConfigStore()
			if err != nil {
				return err
			}
			if _, ok := store.GetClipByName(name); ok {
				return fmt.Errorf("clip %q already registered in config (use 'clip upgrade' instead)", name)
			}
		}

		// 解压
		if err := extractZip(r.File, instanceDir); err != nil {
			os.RemoveAll(instanceDir)
			return fmt.Errorf("extract clip: %w", err)
		}

		// seed/ → data/ 初始化
		seedDir := filepath.Join(instanceDir, "seed")
		dataDir := filepath.Join(instanceDir, "data")
		if info, err := os.Stat(seedDir); err == nil && info.IsDir() {
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				return fmt.Errorf("create data dir: %w", err)
			}
			if err := copyDirContents(seedDir, dataDir); err != nil {
				return fmt.Errorf("init data from seed: %w", err)
			}
			os.RemoveAll(seedDir)
		}

		// 注册到 config.yaml
		store, err := openConfigStore()
		if err != nil {
			return err
		}
		entry, err := store.AddClip(name, instanceDir)
		if err != nil {
			return fmt.Errorf("register clip: %w", err)
		}

		fmt.Printf("installed %s v%s → %s (clip_id: %s)\n", name, meta.Version, instanceDir, entry.ID)
		return nil
	},
}

var clipUpgradeCmd = &cobra.Command{
	Use:   "upgrade <file.clip>",
	Short: "Upgrade an installed clip (preserves data/)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		clipFile := args[0]

		// 打开 zip 包
		r, err := zip.OpenReader(clipFile)
		if err != nil {
			return fmt.Errorf("open clip package: %w", err)
		}
		defer r.Close()

		// 读取新版本 clip.yaml
		meta, err := readClipMetaFromZip(r.File)
		if err != nil {
			return err
		}

		// 查找已安装实例
		store, err := openConfigStore()
		if err != nil {
			return err
		}
		clip, ok := store.GetClipByName(meta.Name)
		if !ok {
			return fmt.Errorf("clip %q not installed (use 'clip install' first)", meta.Name)
		}
		instanceDir := clip.Workdir

		// 读取旧版本号
		oldVersion := "unknown"
		if oldMeta, err := readClipMetaFromDir(instanceDir); err == nil {
			oldVersion = oldMeta.Version
		}

		// 删除代码层（保留 data/）
		for _, entry := range codeLayerEntries {
			os.RemoveAll(filepath.Join(instanceDir, entry))
		}

		// 解压新版本
		if err := extractZip(r.File, instanceDir); err != nil {
			return fmt.Errorf("extract clip: %w", err)
		}

		// 移除 seed/（升级不重新初始化数据）
		os.RemoveAll(filepath.Join(instanceDir, "seed"))

		fmt.Printf("upgraded %s: v%s → v%s\n", meta.Name, oldVersion, meta.Version)
		return nil
	},
}

var clipUninstallKeepData bool

var clipUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall a clip instance",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		// 查找实例
		store, err := openConfigStore()
		if err != nil {
			return err
		}
		clip, ok := store.GetClipByName(name)
		if !ok {
			return fmt.Errorf("clip %q not found", name)
		}

		// 吊销所有关联 token
		revoked, err := store.RevokeTokensByClipID(clip.ID)
		if err != nil {
			return fmt.Errorf("revoke tokens: %w", err)
		}

		// 从 config 中移除
		if _, err := store.DeleteClip(clip.ID); err != nil {
			return fmt.Errorf("delete clip config: %w", err)
		}

		// 清理文件
		if clipUninstallKeepData {
			for _, entry := range codeLayerEntries {
				os.RemoveAll(filepath.Join(clip.Workdir, entry))
			}
			fmt.Printf("uninstalled %s (data/ preserved at %s)\n", name, filepath.Join(clip.Workdir, "data"))
		} else {
			os.RemoveAll(clip.Workdir)
			fmt.Printf("uninstalled %s\n", name)
		}

		if revoked > 0 {
			fmt.Printf("revoked %d token(s)\n", revoked)
		}
		return nil
	},
}

func init() {
	clipInstallCmd.Flags().StringVar(&clipInstallName, "name", "", "override instance name")
	clipUninstallCmd.Flags().BoolVar(&clipUninstallKeepData, "keep-data", false, "preserve data/ directory")

	clipCmd.AddCommand(clipInstallCmd, clipUpgradeCmd, clipUninstallCmd)
}

// --- helpers ---

func openConfigStore() (*config.Store, error) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	return config.NewStore(cfgPath)
}

func defaultClipsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "pinix", "clips"), nil
}

func readClipMetaFromZip(files []*zip.File) (*clipMeta, error) {
	for _, f := range files {
		if f.Name == "clip.yaml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open clip.yaml: %w", err)
			}
			defer rc.Close()

			var meta clipMeta
			if err := yaml.NewDecoder(rc).Decode(&meta); err != nil {
				return nil, fmt.Errorf("parse clip.yaml: %w", err)
			}
			if meta.Name == "" {
				return nil, fmt.Errorf("clip.yaml: name is required")
			}
			if meta.Version == "" {
				return nil, fmt.Errorf("clip.yaml: version is required")
			}
			return &meta, nil
		}
	}
	return nil, fmt.Errorf("clip.yaml not found in package")
}

func readClipMetaFromDir(dir string) (*clipMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "clip.yaml"))
	if err != nil {
		return nil, err
	}
	var meta clipMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func extractZip(files []*zip.File, destDir string) error {
	cleanDest := filepath.Clean(destDir)

	for _, f := range files {
		destPath := filepath.Clean(filepath.Join(destDir, f.Name))

		// 防止路径穿越攻击
		if destPath != cleanDest && !strings.HasPrefix(destPath, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}

		if err := extractZipFile(f, destPath); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

func copyDirContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		if e.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			if err := copyDirContents(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
