package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/server"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var addCmd = &cobra.Command{
	Use:   "add [path...]",
	Short: "Add files or directories to the index",
	Long: `Checks the configured docs directory (not the current working directory) for untracked and modified files, adding them to the index ready for push.

Each path may be a file or a directory. A directory is added recursively (hidden files are skipped), and
keeps its layout, so 'notes/sub/b.html' becomes the document 'notes/sub/b'. Paths outside the docs
directory are symlinked into it first.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := pbconfig.Load()
		if err != nil {
			ui.PrintErr("failed to load config", err.Error(), nil)
			os.Exit(1)
		}

		if cfg.DocsDir == "" {
			ui.PrintErr("configured docs directory is empty", "run 'pagebox init' to configure", nil)
			os.Exit(1)
		}

		// Ensure DocsDir exists
		if err := os.MkdirAll(cfg.DocsDir, 0755); err != nil {
			ui.PrintErr("failed to access configured docs directory", err.Error(), nil)
			os.Exit(1)
		}

		idx, err := index.NewStore()
		if err != nil {
			ui.PrintErr("failed to open index", err.Error(), nil)
			os.Exit(1)
		}

		allFlag, _ := cmd.Flags().GetBool("all")
		cwd, _ := os.Getwd()

		// Resolve which files to inspect within cfg.DocsDir
		filesToInspect, err := resolveFilesToInspect(cfg.DocsDir, cwd, args, allFlag)
		if err != nil {
			ui.PrintErr("failed to resolve files to add", err.Error(), nil)
			os.Exit(1)
		}

		var added []string

		for _, rel := range filesToInspect {
			fullPath := filepath.Join(cfg.DocsDir, filepath.FromSlash(rel))
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}
			if info.IsDir() {
				continue
			}

			hashHex, size, err := computeFileHash(fullPath)
			if err != nil {
				ui.PrintWarn(fmt.Sprintf("could not read %s", rel), err.Error(), nil)
				continue
			}

			id := index.TrimDocExt(rel)

			entry, exists := idx.Get(id)
			if !exists {
				// Untracked -> Add as StateNew
				_ = idx.Update(id, func(e *index.Entry) {
					e.ID = id
					e.Key = rel
					e.State = index.StateNew
					e.Size = size
					e.LocalSHA256 = hashHex
					e.LastSyncedAt = time.Time{}
				})
				added = append(added, rel)
			} else {
				// Already in index: check if modified locally
				if entry.LocalSHA256 != hashHex || entry.State == index.StateNew || entry.State == index.StateDirty || entry.Key == "" || entry.State == "" {
					_ = idx.Update(id, func(e *index.Entry) {
						if e.Key == "" {
							e.Key = rel
						}
						if e.State == "" {
							e.State = index.StateNew
						} else if e.State == index.StateCached {
							e.State = index.StateDirty
						}
						e.Size = size
						e.LocalSHA256 = hashHex
					})
					// Only output if hash changed, state was cached (i.e. real change), or explicitly passed via args
					if entry.LocalSHA256 != hashHex || entry.State == index.StateCached || len(args) > 0 {
						added = append(added, rel)
					}
				}
			}
		}

		if len(added) > 0 {
			sort.Strings(added)
			for _, file := range added {
				fmt.Printf("add '%s'\n", file)
			}

			if err := server.WriteIndexFile(cfg, idx); err != nil {
				ui.PrintWarn("failed to update index.html", err.Error(), nil)
			}
			_ = server.WriteRobotsFile(cfg)

			if cfg.IsS3Configured() {
				store, err := storageNew(cfg)
				if err != nil {
					ui.PrintErr("failed to init storage for s3 upload", err.Error(), nil)
					os.Exit(1)
				}
				for _, file := range added {
					id := index.TrimDocExt(file)
					if err := pushDocument(id, cfg, store, idx); err != nil {
						os.Exit(1)
					}
				}
				updateAndPushIndexHTML(cfg, store, idx)
			} else {
				ui.Hint("push changes to remote storage:", nil)
				ui.Command("pagebox push", nil)
			}
		} else {
			fmt.Println("nothing to add")
		}
	},
}

func init() {
	addCmd.Flags().BoolP("all", "A", false, "add all files in configured docs directory")
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	} else if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func symlinkToDocsDir(docsDir, srcPath string) (string, error) {
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return "", err
	}

	name := filepath.Base(srcPath)
	destPath := filepath.Join(docsDir, name)

	createLink := true
	if linfo, err := os.Lstat(destPath); err == nil {
		if linfo.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(destPath)
			if err == nil && target == srcPath {
				createLink = false
			} else {
				_ = os.Remove(destPath)
			}
		} else {
			// Never delete real documents to make room for a link.
			return "", fmt.Errorf("%s already exists in the docs directory and is not a symlink; rename or move it first", name)
		}
	}

	if createLink {
		if err := os.Symlink(srcPath, destPath); err != nil {
			return "", err
		}
		ui.PrintOk("created symlink", fmt.Sprintf("'%s' -> '%s'", name, srcPath), nil)
	}

	return destPath, nil
}

func resolveFilesToInspect(docsDir, cwd string, args []string, allFlag bool) ([]string, error) {
	// If no args, "." arg, or --all flag, scan the entire configured docs directory
	if allFlag || len(args) == 0 || (len(args) == 1 && args[0] == ".") {
		return collectAllDocFiles(docsDir)
	}

	var results []string
	seen := make(map[string]bool)

	for _, rawArg := range args {
		arg := expandHome(rawArg)
		var target string

		// 1. If arg is absolute
		if filepath.IsAbs(arg) {
			cleanArg := filepath.Clean(arg)
			rel, err := filepath.Rel(docsDir, cleanArg)
			if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
				// Absolute path inside docsDir
				target = cleanArg
			} else if _, err := os.Stat(cleanArg); err == nil {
				// Absolute path outside docsDir, exists!
				dest, err := symlinkToDocsDir(docsDir, cleanArg)
				if err != nil {
					ui.PrintErr("failed to create symlink", err.Error(), nil)
					continue
				}
				target = dest
			}
		} else {
			// 2. Relative path: first check directly inside docsDir
			directTarget := filepath.Join(docsDir, arg)
			if _, err := os.Stat(directTarget); err == nil {
				target = directTarget
			} else {
				// 3. Check relative to cwd
				absFromCwd := filepath.Clean(filepath.Join(cwd, arg))
				if _, err := os.Stat(absFromCwd); err == nil {
					rel, err := filepath.Rel(docsDir, absFromCwd)
					if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
						target = absFromCwd
					} else {
						// Outside docsDir, exists!
						dest, err := symlinkToDocsDir(docsDir, absFromCwd)
						if err != nil {
							ui.PrintErr("failed to create symlink", err.Error(), nil)
							continue
						}
						target = dest
					}
				}
			}
		}

		if target == "" {
			ui.PrintWarn("path not found in configured docs dir", fmt.Sprintf("'%s' (docs dir: %s)", rawArg, docsDir), nil)
			continue
		}

		info, err := os.Stat(target)
		if err != nil {
			ui.PrintWarn("path not found in configured docs dir", fmt.Sprintf("'%s' (docs dir: %s)", rawArg, docsDir), nil)
			continue
		}

		if info.IsDir() {
			files, err := collectDirDocFiles(docsDir, target)
			if err != nil {
				return nil, err
			}
			for _, f := range files {
				if !seen[f] {
					seen[f] = true
					results = append(results, f)
				}
			}
		} else {
			rel, err := filepath.Rel(docsDir, target)
			if err == nil {
				slashRel := filepath.ToSlash(rel)
				if !seen[slashRel] && !isIgnored(slashRel) {
					seen[slashRel] = true
					results = append(results, slashRel)
				}
			}
		}
	}

	return results, nil
}

func collectAllDocFiles(docsDir string) ([]string, error) {
	return collectDirDocFiles(docsDir, docsDir)
}

func collectDirDocFiles(docsDir, targetDir string) ([]string, error) {
	var files []string
	visited := make(map[string]bool)

	var walk func(currentDir string) error
	walk = func(currentDir string) error {
		realCurrent, err := filepath.EvalSymlinks(currentDir)
		if err != nil {
			return nil
		}
		if visited[realCurrent] {
			return nil
		}
		visited[realCurrent] = true

		entries, err := os.ReadDir(currentDir)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}

			fullPath := filepath.Join(currentDir, name)
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}

			if info.IsDir() {
				if err := walk(fullPath); err != nil {
					return err
				}
			} else {
				rel, err := filepath.Rel(docsDir, fullPath)
				if err != nil {
					continue
				}
				slashRel := filepath.ToSlash(rel)
				if !isIgnored(slashRel) {
					files = append(files, slashRel)
				}
			}
		}
		return nil
	}

	err := walk(targetDir)
	return files, err
}

func isIgnored(rel string) bool {
	return index.IsIgnoredDoc(index.TrimDocExt(rel), rel)
}

func computeFileHash(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", 0, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, err
	}

	return hex.EncodeToString(h.Sum(nil)), stat.Size(), nil
}
