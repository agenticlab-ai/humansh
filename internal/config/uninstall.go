package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/humansh/humansh/internal/shell"
)

type UninstallOptions struct {
	Purge bool
}

type UninstallResult struct {
	Purged bool
}

type startupRemoval struct {
	path, writePath string
	mode            os.FileMode
	content         []byte
	changed         bool
}

func sameStartupRemoval(left, right startupRemoval) bool {
	return left.path == right.path &&
		left.writePath == right.writePath &&
		left.mode == right.mode &&
		left.changed == right.changed &&
		bytes.Equal(left.content, right.content)
}

// Uninstall removes only paths owned by the current humansh layout. It checks
// every target before applying changes, updates a symlinked startup file at its
// regular-file target, and removes the running binary last.
func Uninstall(paths Paths, options UninstallOptions) (UninstallResult, error) {
	if err := validateCurrentUninstallLayout(paths); err != nil {
		return UninstallResult{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return UninstallResult{}, err
	}
	var layouts []shellInstallLayout

	if _, err := os.Lstat(paths.InstallState); err == nil {
		state, loadErr := LoadInstallState(paths.InstallState)
		if loadErr != nil {
			return UninstallResult{}, fmt.Errorf("install state is invalid: %w", loadErr)
		}
		if state.BinaryPath != paths.Binary {
			return UninstallResult{}, fmt.Errorf("install-state paths do not match the current humansh layout")
		}
		for _, integration := range state.ShellStates() {
			layout, layoutErr := installLayout(paths, shell.ID(integration.Shell), home)
			if layoutErr != nil {
				return UninstallResult{}, layoutErr
			}
			if integration.ShellAssetPath != layout.assetPath || integration.StartupFile != layout.startupFile {
				return UninstallResult{}, fmt.Errorf("install-state paths do not match the current humansh layout")
			}
			layouts = append(layouts, layout)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return UninstallResult{}, err
	}
	if len(layouts) == 0 {
		for _, id := range []shell.ID{shell.Zsh, shell.Bash} {
			layout, layoutErr := installLayout(paths, id, home)
			if layoutErr != nil {
				return UninstallResult{}, layoutErr
			}
			layouts = append(layouts, layout)
		}
	}
	for _, target := range []string{paths.Binary} {
		if err := requireRegularOrAbsent(target); err != nil {
			return UninstallResult{}, err
		}
	}
	startupPlans := make([]startupRemoval, 0, len(layouts))
	for _, layout := range layouts {
		if err := requireRegularOrAbsent(layout.assetPath); err != nil {
			return UninstallResult{}, err
		}
		startupPlan, planErr := planStartupRemoval(layout.startupFile)
		if planErr != nil {
			return UninstallResult{}, planErr
		}
		startupPlans = append(startupPlans, startupPlan)
	}
	if options.Purge {
		for _, directory := range uniquePaths(paths.ConfigDir, paths.DataDir) {
			if err := requireOwnedDirectoryOrAbsent(directory); err != nil {
				return UninstallResult{}, err
			}
		}
	}

	for _, startupPlan := range startupPlans {
		if startupPlan.changed {
			if err := atomicWrite(startupPlan.writePath, startupPlan.content, startupPlan.mode); err != nil {
				return UninstallResult{}, fmt.Errorf("remove managed block from %s: %w", startupPlan.path, err)
			}
		}
	}
	for _, layout := range layouts {
		if err := removeRegularOrAbsent(layout.assetPath); err != nil {
			return UninstallResult{}, err
		}
	}
	for _, target := range []string{paths.InstallState} {
		if err := removeRegularOrAbsent(target); err != nil {
			return UninstallResult{}, err
		}
	}
	for _, layout := range layouts {
		removeEmptyUninstallDirectories(paths, layout.directory)
	}

	if options.Purge {
		if err := DeleteOpenRouterKey(paths); err != nil {
			return UninstallResult{}, fmt.Errorf("remove OpenRouter credential: %w", err)
		}
		for _, directory := range uniquePaths(paths.ConfigDir, paths.DataDir) {
			if err := os.RemoveAll(directory); err != nil {
				return UninstallResult{}, fmt.Errorf("purge %s: %w", directory, err)
			}
		}
	}
	if err := removeRegularOrAbsent(paths.Binary); err != nil {
		return UninstallResult{}, err
	}
	return UninstallResult{Purged: options.Purge}, nil
}

func validateCurrentUninstallLayout(paths Paths) error {
	expected, err := ResolvePaths()
	if err != nil {
		return err
	}
	actualPaths := []string{paths.Binary, paths.DataDir, paths.InstallState, paths.ShellDir, paths.BashShellDir, paths.ConfigDir, paths.ConfigFile, paths.ClassifierFile, paths.Credentials}
	expectedPaths := []string{expected.Binary, expected.DataDir, expected.InstallState, expected.ShellDir, expected.BashShellDir, expected.ConfigDir, expected.ConfigFile, expected.ClassifierFile, expected.Credentials}
	for index, actual := range actualPaths {
		if actual != expectedPaths[index] || !filepath.IsAbs(actual) {
			return fmt.Errorf("refusing uninstall because managed path %q does not match the current humansh layout", actual)
		}
	}
	if filepath.Base(paths.ConfigDir) != "humansh" || filepath.Base(paths.DataDir) != "humansh" {
		return fmt.Errorf("refusing uninstall because an owned directory is not humansh-specific")
	}
	return nil
}

func planStartupRemoval(path string) (startupRemoval, error) {
	plan := startupRemoval{path: path, writePath: path}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return plan, nil
	}
	if err != nil {
		return plan, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(path)
		if readErr != nil {
			return plan, fmt.Errorf("resolve startup-file symlink %s: %w", path, readErr)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		plan.writePath = filepath.Clean(target)
		targetInfo, statErr := os.Lstat(plan.writePath)
		if statErr != nil {
			return plan, fmt.Errorf("startup-file symlink target is unavailable: %w", statErr)
		}
		if targetInfo.Mode()&os.ModeSymlink != 0 {
			return plan, fmt.Errorf("chained startup-file symlinks require manual managed-block removal")
		}
		if !targetInfo.Mode().IsRegular() {
			return plan, fmt.Errorf("startup-file symlink target %s is not a regular file", plan.writePath)
		}
		plan.mode = targetInfo.Mode().Perm()
	} else {
		if !info.Mode().IsRegular() {
			return plan, fmt.Errorf("startup file %s is not a regular file or symlink", path)
		}
		plan.mode = info.Mode().Perm()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return plan, err
	}
	without, err := removeManagedBlock(string(data))
	if err != nil {
		return plan, err
	}
	plan.content = []byte(without)
	plan.changed = without != string(data)
	return plan, nil
}

func requireRegularOrAbsent(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("managed target %s is not a regular file; no files were changed", path)
	}
	return nil
}

func removeRegularOrAbsent(path string) error {
	if err := requireRegularOrAbsent(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func requireOwnedDirectoryOrAbsent(path string) error {
	if !filepath.IsAbs(path) || filepath.Base(path) != "humansh" || filepath.Dir(path) == string(filepath.Separator) {
		return fmt.Errorf("refusing unsafe purge path %q", path)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("purge target %s is not a regular directory", path)
	}
	return nil
}

func uniquePaths(paths ...string) []string {
	seen := make(map[string]bool, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	return result
}

func removeEmptyUninstallDirectories(paths Paths, shellDirectory string) {
	for _, directory := range []string{shellDirectory, filepath.Dir(shellDirectory), paths.DataDir} {
		_ = os.Remove(directory)
	}
}
