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
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var addCmd = &cobra.Command{
	Use:   "add [path...]",
	Short: "Add files in configured directory to the index",
	Long:  "Checks the configured docs directory (not the current working directory) for untracked and modified files, adding them to the index ready for push.",
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
				if entry.LocalSHA256 != hashHex || entry.State == index.StateNew || entry.State == index.StateDirty {
					_ = idx.Update(id, func(e *index.Entry) {
						if e.State == index.StateCached {
							e.State = index.StateDirty
						}
						e.Size = size
						e.LocalSHA256 = hashHex
					})
					// Only output if hash changed or state was cached (i.e. real change)
					if entry.LocalSHA256 != hashHex || entry.State == index.StateCached {
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
			ui.Hint("push changes to remote storage:", nil)
			ui.Command("pagebox push", nil)
		} else {
			fmt.Println("nothing to add")
		}
	},
}

func init() {
	addCmd.Flags().BoolP("all", "A", false, "add all files in configured docs directory")
}

func resolveFilesToInspect(docsDir, cwd string, args []string, allFlag bool) ([]string, error) {
	// If no args, "." arg, or --all flag, scan the entire configured docs directory
	if allFlag || len(args) == 0 || (len(args) == 1 && args[0] == ".") {
		return collectAllDocFiles(docsDir)
	}

	var results []string
	seen := make(map[string]bool)

	for _, arg := range args {
		// 1. Try directly inside docsDir
		target := filepath.Join(docsDir, arg)
		if _, err := os.Stat(target); err != nil {
			// 2. Try if arg is absolute and inside docsDir
			if filepath.IsAbs(arg) {
				rel, err := filepath.Rel(docsDir, arg)
				if err == nil && !strings.HasPrefix(rel, "..") {
					target = arg
				}
			} else if cwd != "" {
				// 3. Try relative to cwd if cwd is within docsDir
				absFromCwd := filepath.Join(cwd, arg)
				rel, err := filepath.Rel(docsDir, absFromCwd)
				if err == nil && !strings.HasPrefix(rel, "..") {
					target = absFromCwd
				}
			}
		}

		info, err := os.Stat(target)
		if err != nil {
			ui.PrintWarn("path not found in configured docs dir", fmt.Sprintf("'%s' (docs dir: %s)", arg, docsDir), nil)
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
	err := filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != targetDir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		rel, err := filepath.Rel(docsDir, path)
		if err != nil {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if !isIgnored(slashRel) {
			files = append(files, slashRel)
		}
		return nil
	})
	return files, err
}

func isIgnored(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, ".") {
			return true
		}
	}
	return false
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
