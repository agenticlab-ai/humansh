package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agenticlab-ai/humansh/assets"
	"github.com/agenticlab-ai/humansh/internal/shell"
	"github.com/agenticlab-ai/humansh/internal/shell/protocol"
	"golang.org/x/sys/unix"
)

const (
	managedStart = "# >>> humansh >>>"
	managedEnd   = "# <<< humansh <<<"
)

type InstallState struct {
	Version          int
	BinaryPath       string
	InstalledVersion string
	Integrations     []ShellInstallState
	// Legacy single-shell fields are populated when reading version 1 state and
	// mirror the first integration for callers migrating to version 2.
	Shell               string
	Protocol            string
	ShellAssetPath      string
	ShellAssetSHA256    string
	StartupFile         string
	ManagedBlockVersion int
}

type ShellInstallState struct {
	Shell            string
	Protocol         string
	ShellAssetPath   string
	ShellAssetSHA256 string
	StartupFile      string
}

type shellInstallLayout struct {
	id          shell.ID
	protocol    string
	directory   string
	assetPath   string
	asset       []byte
	startupFile string
}

type shellRemovalPlan struct {
	layout shellInstallLayout
	plan   startupRemoval
}

func installLayout(paths Paths, id shell.ID, home string) (shellInstallLayout, error) {
	switch id {
	case shell.Zsh:
		return shellInstallLayout{id: id, protocol: protocol.Version, directory: paths.ShellDir, assetPath: filepath.Join(paths.ShellDir, "humansh.zsh"), asset: assets.ZshIntegration, startupFile: filepath.Join(home, ".zshrc")}, nil
	case shell.Bash:
		return shellInstallLayout{id: id, protocol: protocol.ReadlineVersion, directory: paths.BashShellDir, assetPath: filepath.Join(paths.BashShellDir, "humansh.bash"), asset: assets.BashIntegration, startupFile: filepath.Join(home, ".bashrc")}, nil
	default:
		return shellInstallLayout{}, fmt.Errorf("unsupported shell %q", id)
	}
}

type SetupOptions struct {
	NoShellChange           bool
	Repair                  bool
	Shells                  []shell.ID
	ReviewedStartups        []StartupChange
	ReviewedRemovals        []StartupChange
	ReviewedStartup         *StartupChange
	ReviewedPreviousStartup *StartupChange
}

// StartupChange is the exact startup-file rewrite prepared for setup review.
// SetupWithOptions verifies a supplied ReviewedStartup again immediately before
// applying it so a file changed after review is never silently overwritten.
type StartupChange struct {
	Path       string
	TargetPath string

	before      []byte
	after       []byte
	existed     bool
	mode        os.FileMode
	needsBackup bool
}

// StartupPatchLine is one line in the user-visible humansh managed-block patch.
// Kind is '+' for an addition and '-' for a removal.
type StartupPatchLine struct {
	Kind byte
	Text string
}

type startupAccessError struct {
	message string
	cause   error
}

func (err *startupAccessError) Error() string {
	if err.cause == nil {
		return err.message
	}
	return err.message + ": " + err.cause.Error()
}

func (err *startupAccessError) Unwrap() error {
	return err.cause
}

func IsStartupAccessError(err error) bool {
	var accessErr *startupAccessError
	return errors.As(err, &accessErr)
}

func (change StartupChange) Changed() bool {
	return !bytes.Equal(change.before, change.after)
}

// PatchLines returns only managed humansh lines, never unrelated startup-file
// content. It is therefore safe to show during setup even when .zshrc contains
// private environment configuration.
func (change StartupChange) PatchLines() []StartupPatchLine {
	before := managedStartupLines(string(change.before))
	afterBlock := managedBlockText(string(change.after))
	var after []string
	if afterBlock != "" {
		after = strings.Split(afterBlock, "\n")
	}
	if len(before) == 0 {
		patch := make([]StartupPatchLine, 0, len(after))
		for _, line := range after {
			patch = append(patch, StartupPatchLine{Kind: '+', Text: line})
		}
		return patch
	}
	return changedManagedBlockLines(before, after)
}

func (change StartupChange) RepositionsManagedBlock() bool {
	beforeBlock := managedBlockText(string(change.before))
	afterBlock := managedBlockText(string(change.after))
	return beforeBlock != "" && beforeBlock == afterBlock && change.Changed()
}

func Setup(paths Paths, cfg RuntimeConfig, binaryVersion string) (InstallState, error) {
	return SetupWithOptions(paths, cfg, binaryVersion, SetupOptions{})
}

func SetupWithOptions(paths Paths, cfg RuntimeConfig, binaryVersion string, options SetupOptions) (state InstallState, resultErr error) {
	if err := cfg.Validate(); err != nil {
		return InstallState{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return InstallState{}, err
	}
	targetIDs := options.Shells
	if len(targetIDs) == 0 {
		targetIDs = []shell.ID{cfg.Shell.Name}
	}
	targetIDs, err = normalizeShellTargets(targetIDs)
	if err != nil {
		return InstallState{}, err
	}
	targets := make(map[shell.ID]shellInstallLayout, len(targetIDs))
	targetConfigs := make(map[shell.ID]RuntimeConfig, len(targetIDs))
	for _, id := range targetIDs {
		layout, layoutErr := installLayout(paths, id, home)
		if layoutErr != nil {
			return InstallState{}, layoutErr
		}
		shellCfg := configForShell(cfg, id)
		if validateErr := shellCfg.Validate(); validateErr != nil {
			return InstallState{}, validateErr
		}
		targets[id] = layout
		targetConfigs[id] = shellCfg
	}
	var removals []shellRemovalPlan
	if _, stateErr := os.Lstat(paths.InstallState); stateErr == nil {
		installed, loadErr := LoadInstallState(paths.InstallState)
		if loadErr != nil {
			if !options.Repair {
				return InstallState{}, fmt.Errorf("existing install state is invalid: %w", loadErr)
			}
		} else {
			if installed.BinaryPath != paths.Binary {
				return InstallState{}, fmt.Errorf("existing install-state paths do not match the current humansh layout")
			}
			for _, prior := range installed.ShellStates() {
				oldLayout, layoutErr := installLayout(paths, shell.ID(prior.Shell), home)
				if layoutErr != nil {
					return InstallState{}, layoutErr
				}
				if prior.ShellAssetPath != oldLayout.assetPath || prior.StartupFile != oldLayout.startupFile {
					return InstallState{}, fmt.Errorf("existing install-state paths do not match the current humansh layout")
				}
				if _, retained := targets[oldLayout.id]; retained {
					continue
				}
				if options.NoShellChange {
					return InstallState{}, fmt.Errorf("cannot switch shell integrations with --no-shell-change because the %s startup block would remain active", oldLayout.id)
				}
				removal, removalErr := planStartupRemoval(oldLayout.startupFile)
				if removalErr != nil {
					return InstallState{}, fmt.Errorf("prepare removal of the old %s integration: %w", oldLayout.id, removalErr)
				}
				removals = append(removals, shellRemovalPlan{layout: oldLayout, plan: removal})
			}
		}
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		return InstallState{}, stateErr
	}
	if options.ReviewedPreviousStartup != nil {
		options.ReviewedRemovals = append(options.ReviewedRemovals, *options.ReviewedPreviousStartup)
	}
	for _, removal := range removals {
		change, changeErr := startupRemovalChange(removal.plan)
		if changeErr != nil {
			return InstallState{}, changeErr
		}
		if reviewed := reviewedChangeForPath(options.ReviewedRemovals, change.Path); reviewed != nil && !sameStartupChange(*reviewed, change) {
			return InstallState{}, fmt.Errorf("%s changed after the setup review; rerun `humansh setup` to review the new patch", change.Path)
		}
	}
	if err := rejectUnmatchedReviews(options.ReviewedRemovals, removals); err != nil {
		return InstallState{}, err
	}
	startupChanges := make(map[shell.ID]StartupChange, len(targetIDs))
	if !options.NoShellChange {
		if options.ReviewedStartup != nil {
			options.ReviewedStartups = append(options.ReviewedStartups, *options.ReviewedStartup)
		}
		for _, id := range targetIDs {
			layout := targets[id]
			change, planErr := planManagedStartup(layout.startupFile, paths, targetConfigs[id], options.Repair)
			if planErr != nil {
				return InstallState{}, planErr
			}
			if reviewed := reviewedChangeForPath(options.ReviewedStartups, change.Path); reviewed != nil && !sameStartupChange(*reviewed, change) {
				return InstallState{}, fmt.Errorf("%s changed after the setup review; rerun `humansh setup` to review the new patch", change.Path)
			}
			startupChanges[id] = change
		}
		for _, reviewed := range options.ReviewedStartups {
			matched := false
			for _, change := range startupChanges {
				matched = matched || reviewed.Path == change.Path
			}
			if !matched {
				return InstallState{}, fmt.Errorf("installed shells changed after the setup review; rerun `humansh setup`")
			}
		}
	}
	type managedPath struct {
		path          string
		followSymlink bool
	}
	managedPaths := []managedPath{{path: paths.InstallState}}
	for _, id := range targetIDs {
		layout := targets[id]
		managedPaths = append(managedPaths, managedPath{path: layout.assetPath})
		if !options.NoShellChange {
			managedPaths = append(managedPaths, managedPath{path: layout.startupFile, followSymlink: true})
		}
	}
	for _, removal := range removals {
		managedPaths = append(managedPaths, managedPath{path: removal.layout.assetPath}, managedPath{path: removal.layout.startupFile, followSymlink: true})
	}
	snapshots := make([]fileSnapshot, 0, len(managedPaths))
	snapshotted := map[string]bool{}
	for _, managed := range managedPaths {
		key := fmt.Sprintf("%t:%s", managed.followSymlink, managed.path)
		if snapshotted[key] {
			continue
		}
		snapshotted[key] = true
		snapshot, snapshotErr := snapshotFile(managed.path, managed.followSymlink)
		if snapshotErr != nil {
			return InstallState{}, snapshotErr
		}
		snapshots = append(snapshots, snapshot)
	}
	defer func() {
		if resultErr == nil {
			return
		}
		var rollbackErr error
		for index := len(snapshots) - 1; index >= 0; index-- {
			rollbackErr = errors.Join(rollbackErr, snapshots[index].restore())
		}
		if rollbackErr != nil {
			resultErr = fmt.Errorf("%w; managed-file rollback failed: %v", resultErr, rollbackErr)
		}
	}()
	for _, id := range targetIDs {
		layout := targets[id]
		if err := os.MkdirAll(layout.directory, 0o755); err != nil {
			return InstallState{}, err
		}
		if err := atomicWrite(layout.assetPath, layout.asset, 0o644); err != nil {
			return InstallState{}, err
		}
	}
	for _, removal := range removals {
		latestRemoval, planErr := planStartupRemoval(removal.layout.startupFile)
		if planErr != nil {
			return InstallState{}, planErr
		}
		if !sameStartupRemoval(removal.plan, latestRemoval) {
			return InstallState{}, fmt.Errorf("%s changed while setup was applying the reviewed patch; rerun `humansh setup`", removal.layout.startupFile)
		}
		if latestRemoval.changed {
			if err := atomicWrite(latestRemoval.writePath, latestRemoval.content, latestRemoval.mode); err != nil {
				return InstallState{}, fmt.Errorf("remove old humansh block from %s: %w", latestRemoval.path, err)
			}
		}
		if err := removeRegularOrAbsent(removal.layout.assetPath); err != nil {
			return InstallState{}, err
		}
	}
	if !options.NoShellChange {
		for _, id := range targetIDs {
			layout := targets[id]
			latest, planErr := planManagedStartup(layout.startupFile, paths, targetConfigs[id], options.Repair)
			if planErr != nil {
				return InstallState{}, planErr
			}
			if !sameStartupChange(startupChanges[id], latest) {
				return InstallState{}, fmt.Errorf("%s changed while setup was applying the reviewed patch; rerun `humansh setup`", layout.startupFile)
			}
			if err := applyStartupChange(latest); err != nil {
				return InstallState{}, err
			}
		}
	}
	state = InstallState{Version: 2, BinaryPath: paths.Binary, InstalledVersion: binaryVersion, ManagedBlockVersion: 1}
	for _, id := range targetIDs {
		layout := targets[id]
		state.Integrations = append(state.Integrations, ShellInstallState{Shell: string(id), Protocol: layout.protocol, ShellAssetPath: layout.assetPath, ShellAssetSHA256: fmt.Sprintf("%x", sha256.Sum256(layout.asset)), StartupFile: layout.startupFile})
	}
	state.populateLegacyFields()
	if err := atomicWrite(paths.InstallState, []byte(renderInstallState(state)), 0o600); err != nil {
		return InstallState{}, err
	}
	for _, removal := range removals {
		_ = os.Remove(removal.layout.directory)
	}
	_ = os.Remove(filepath.Join(paths.DataDir, "shell"))
	return state, nil
}

func normalizeShellTargets(ids []shell.ID) ([]shell.ID, error) {
	wanted := make(map[shell.ID]bool, len(ids))
	for _, id := range ids {
		if id != shell.Zsh && id != shell.Bash {
			return nil, fmt.Errorf("unsupported shell %q", id)
		}
		wanted[id] = true
	}
	var normalized []shell.ID
	for _, id := range []shell.ID{shell.Zsh, shell.Bash} {
		if wanted[id] {
			normalized = append(normalized, id)
		}
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one supported shell integration is required")
	}
	return normalized, nil
}

func configForShell(cfg RuntimeConfig, id shell.ID) RuntimeConfig {
	cfg.Shell.Name = id
	switch id {
	case shell.Zsh:
		cfg.Shell.Protocol = protocol.Version
	case shell.Bash:
		cfg.Shell.Protocol = protocol.ReadlineVersion
		cfg.Shell.SmartEnter = false
	}
	return cfg
}

func reviewedChangeForPath(changes []StartupChange, path string) *StartupChange {
	for index := range changes {
		if changes[index].Path == path {
			return &changes[index]
		}
	}
	return nil
}

func rejectUnmatchedReviews(changes []StartupChange, removals []shellRemovalPlan) error {
	for _, change := range changes {
		matched := false
		for _, removal := range removals {
			matched = matched || change.Path == removal.layout.startupFile
		}
		if !matched {
			return fmt.Errorf("installed shells changed after the setup review; rerun `humansh setup`")
		}
	}
	return nil
}

type fileSnapshot struct {
	path, writePath string
	existed         bool
	data            []byte
	mode            os.FileMode
}

func snapshotFile(path string, followSymlink bool) (fileSnapshot, error) {
	snapshot := fileSnapshot{path: path, writePath: path}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if !followSymlink {
			return snapshot, fmt.Errorf("managed file %s must not be a symlink", path)
		}
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			return snapshot, fmt.Errorf("resolve managed-file symlink %s: %w", path, resolveErr)
		}
		snapshot.writePath = resolved
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	targetInfo, err := os.Stat(path)
	if err != nil {
		return snapshot, err
	}
	if !targetInfo.Mode().IsRegular() {
		return snapshot, fmt.Errorf("managed path %s is not a regular file", path)
	}
	snapshot.existed, snapshot.data, snapshot.mode = true, data, targetInfo.Mode().Perm()
	return snapshot, nil
}

func (s fileSnapshot) restore() error {
	if s.existed {
		return atomicWrite(s.writePath, s.data, s.mode)
	}
	if err := os.Remove(s.writePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ManagedBlock(paths Paths, cfg RuntimeConfig) string {
	return managedBlock(paths, cfg)
}

func ManagedBlockForShell(paths Paths, cfg RuntimeConfig, id shell.ID) string {
	return managedBlock(paths, configForShell(cfg, id))
}

// PreviewStartupChange prepares the exact shell startup-file change setup will apply. It
// reads but never writes the startup file.
func PreviewStartupChange(paths Paths, cfg RuntimeConfig, repair bool) (StartupChange, error) {
	changes, err := PreviewStartupChanges(paths, cfg, []shell.ID{cfg.Shell.Name}, repair)
	if err != nil {
		return StartupChange{}, err
	}
	return changes[0], nil
}

func PreviewStartupChanges(paths Paths, cfg RuntimeConfig, ids []shell.ID, repair bool) ([]StartupChange, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ids, err := normalizeShellTargets(ids)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	changes := make([]StartupChange, 0, len(ids))
	for _, id := range ids {
		layout, layoutErr := installLayout(paths, id, home)
		if layoutErr != nil {
			return nil, layoutErr
		}
		change, planErr := planManagedStartup(layout.startupFile, paths, configForShell(cfg, id), repair)
		if planErr != nil {
			return nil, planErr
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// PreviewPreviousStartupChange returns the exact old startup-file removal that
// a shell switch will apply. A non-nil result means setup is switching shells,
// even when the prior managed block is already absent.
func PreviewPreviousStartupChange(paths Paths, cfg RuntimeConfig, repair bool) (*StartupChange, error) {
	changes, err := PreviewRemovedStartupChanges(paths, []shell.ID{cfg.Shell.Name}, repair)
	if err != nil || len(changes) == 0 {
		return nil, err
	}
	if len(changes) > 1 {
		return nil, fmt.Errorf("multiple installed shell integrations would be removed; rerun `humansh setup`")
	}
	return &changes[0], nil
}

func PreviewRemovedStartupChanges(paths Paths, targetIDs []shell.ID, repair bool) ([]StartupChange, error) {
	targetIDs, err := normalizeShellTargets(targetIDs)
	if err != nil {
		return nil, err
	}
	targets := map[shell.ID]bool{}
	for _, id := range targetIDs {
		targets[id] = true
	}
	if _, err := os.Lstat(paths.InstallState); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	installed, err := LoadInstallState(paths.InstallState)
	if err != nil {
		if repair {
			return nil, nil
		}
		return nil, fmt.Errorf("existing install state is invalid: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if installed.BinaryPath != paths.Binary {
		return nil, fmt.Errorf("existing install-state paths do not match the current humansh layout")
	}
	var changes []StartupChange
	for _, integration := range installed.ShellStates() {
		id := shell.ID(integration.Shell)
		if targets[id] {
			continue
		}
		layout, layoutErr := installLayout(paths, id, home)
		if layoutErr != nil {
			return nil, layoutErr
		}
		if integration.ShellAssetPath != layout.assetPath || integration.StartupFile != layout.startupFile {
			return nil, fmt.Errorf("existing install-state paths do not match the current humansh layout")
		}
		removal, removalErr := planStartupRemoval(layout.startupFile)
		if removalErr != nil {
			return nil, removalErr
		}
		change, changeErr := startupRemovalChange(removal)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func startupRemovalChange(removal startupRemoval) (StartupChange, error) {
	change := StartupChange{Path: removal.path, TargetPath: removal.writePath, after: append([]byte(nil), removal.content...), mode: removal.mode}
	data, err := os.ReadFile(removal.path)
	if errors.Is(err, os.ErrNotExist) {
		return change, nil
	}
	if err != nil {
		return StartupChange{}, err
	}
	change.before = data
	change.existed = true
	return change, nil
}

func planManagedStartup(path string, paths Paths, cfg RuntimeConfig, repair bool) (StartupChange, error) {
	change := StartupChange{Path: path, TargetPath: path, mode: 0o644}
	if linkInfo, linkErr := os.Lstat(path); linkErr == nil && linkInfo.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			if errors.Is(resolveErr, os.ErrPermission) {
				return StartupChange{}, &startupAccessError{message: fmt.Sprintf("cannot resolve startup-file symlink %s", path), cause: resolveErr}
			}
			return StartupChange{}, fmt.Errorf("resolve startup-file symlink %s: %w", path, resolveErr)
		}
		change.TargetPath = resolved
	} else if linkErr != nil && !os.IsNotExist(linkErr) {
		return StartupChange{}, linkErr
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		if errors.Is(err, os.ErrPermission) {
			return StartupChange{}, &startupAccessError{message: fmt.Sprintf("cannot read startup file %s", path), cause: err}
		}
		return StartupChange{}, fmt.Errorf("read startup file %s: %w", path, err)
	}
	if info, statErr := os.Stat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return StartupChange{}, fmt.Errorf("startup path %s is not a regular file", path)
		}
		change.existed = true
		change.mode = info.Mode().Perm()
		if change.mode&0o200 == 0 {
			return StartupChange{}, &startupAccessError{message: fmt.Sprintf("%s is not owner-writable; restore write permission or use `humansh setup --no-shell-change`", path)}
		}
	} else if !os.IsNotExist(statErr) {
		return StartupChange{}, statErr
	}
	if err := unix.Access(filepath.Dir(change.TargetPath), unix.W_OK|unix.X_OK); err != nil {
		return StartupChange{}, &startupAccessError{message: fmt.Sprintf("cannot atomically update %s because %s is not writable", path, filepath.Dir(change.TargetPath)), cause: err}
	}
	original := string(data)
	change.needsBackup = !strings.Contains(original, managedStart) && len(data) > 0
	if change.needsBackup && filepath.Dir(path) != filepath.Dir(change.TargetPath) {
		if err := unix.Access(filepath.Dir(path), unix.W_OK|unix.X_OK); err != nil {
			return StartupChange{}, &startupAccessError{message: fmt.Sprintf("cannot back up %s because %s is not writable", path, filepath.Dir(path)), cause: err}
		}
	}
	without, err := removeManagedBlock(original)
	if err != nil {
		if !repair {
			return StartupChange{}, err
		}
		without = repairManagedBlock(original)
	}
	block := managedBlock(paths, cfg)
	lines := strings.Split(without, "\n")
	insert := len(lines)
	for i, line := range lines {
		if cfg.Shell.Name == shell.Zsh && strings.Contains(line, "zsh-syntax-highlighting") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			insert = i
			break
		}
	}
	newLines := append([]string{}, lines[:insert]...)
	for len(newLines) > 0 && strings.TrimSpace(newLines[len(newLines)-1]) == "" {
		newLines = newLines[:len(newLines)-1]
	}
	if len(newLines) > 0 {
		newLines = append(newLines, "")
	}
	newLines = append(newLines, strings.Split(strings.TrimSuffix(block, "\n"), "\n")...)
	if insert < len(lines) && (len(lines[insert:]) == 0 || strings.TrimSpace(lines[insert]) != "") {
		newLines = append(newLines, "")
	}
	newLines = append(newLines, lines[insert:]...)
	content := strings.Join(newLines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	change.before = append([]byte(nil), data...)
	change.after = []byte(content)
	return change, nil
}

func applyStartupChange(change StartupChange) error {
	if !change.Changed() {
		return nil
	}
	if change.needsBackup {
		backup := change.Path + ".humansh-backup-" + time.Now().Format("20060102-150405.000000000")
		if err := os.WriteFile(backup, change.before, change.mode); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return &startupAccessError{message: fmt.Sprintf("cannot back up startup file %s", change.Path), cause: err}
			}
			return fmt.Errorf("back up %s: %w", change.Path, err)
		}
	}
	if err := atomicWrite(change.TargetPath, change.after, change.mode); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return &startupAccessError{message: fmt.Sprintf("cannot update startup file %s", change.Path), cause: err}
		}
		return fmt.Errorf("update startup file %s: %w", change.Path, err)
	}
	return nil
}

func sameStartupChange(left, right StartupChange) bool {
	return left.Path == right.Path &&
		left.TargetPath == right.TargetPath &&
		left.existed == right.existed &&
		left.mode == right.mode &&
		left.needsBackup == right.needsBackup &&
		bytes.Equal(left.before, right.before) &&
		bytes.Equal(left.after, right.after)
}

func managedBlockText(content string) string {
	start := strings.Index(content, managedStart)
	if start < 0 {
		return ""
	}
	endOffset := strings.Index(content[start:], managedEnd)
	if endOffset < 0 {
		return ""
	}
	return content[start : start+endOffset+len(managedEnd)]
}

func managedStartupLines(content string) []string {
	lines := strings.Split(content, "\n")
	var managed []string
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == managedEnd {
			managed = append(managed, lines[index])
			continue
		}
		if trimmed != managedStart {
			continue
		}

		managed = append(managed, lines[index])
		for index+1 < len(lines) {
			next := strings.TrimSpace(lines[index+1])
			if next == managedEnd {
				index++
				managed = append(managed, lines[index])
				break
			}
			if !managedStartupLine(next) {
				break
			}
			index++
			managed = append(managed, lines[index])
		}
	}
	return managed
}

func changedManagedBlockLines(before, after []string) []StartupPatchLine {
	common := make([][]int, len(before)+1)
	for index := range common {
		common[index] = make([]int, len(after)+1)
	}
	for oldIndex := len(before) - 1; oldIndex >= 0; oldIndex-- {
		for newIndex := len(after) - 1; newIndex >= 0; newIndex-- {
			if before[oldIndex] == after[newIndex] {
				common[oldIndex][newIndex] = common[oldIndex+1][newIndex+1] + 1
			} else if common[oldIndex+1][newIndex] >= common[oldIndex][newIndex+1] {
				common[oldIndex][newIndex] = common[oldIndex+1][newIndex]
			} else {
				common[oldIndex][newIndex] = common[oldIndex][newIndex+1]
			}
		}
	}
	var patch []StartupPatchLine
	for oldIndex, newIndex := 0, 0; oldIndex < len(before) || newIndex < len(after); {
		switch {
		case oldIndex < len(before) && newIndex < len(after) && before[oldIndex] == after[newIndex]:
			oldIndex++
			newIndex++
		case newIndex < len(after) && (oldIndex == len(before) || common[oldIndex][newIndex+1] > common[oldIndex+1][newIndex]):
			patch = append(patch, StartupPatchLine{Kind: '+', Text: after[newIndex]})
			newIndex++
		default:
			patch = append(patch, StartupPatchLine{Kind: '-', Text: before[oldIndex]})
			oldIndex++
		}
	}
	return patch
}

func repairManagedBlock(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inside := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case managedStart:
			inside = true
			continue
		case managedEnd:
			inside = false
			continue
		}
		if inside && managedStartupLine(trimmed) {
			continue
		}
		if inside {
			inside = false
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func managedStartupLine(line string) bool {
	return strings.HasPrefix(line, "export HUMANSH_SMART_ENTER=") ||
		strings.HasPrefix(line, "export HUMANSH_PROVIDER_LABEL=") ||
		strings.HasPrefix(line, "export HUMANSH_CLEAR_LINE_BINDING=") ||
		strings.HasPrefix(line, "export HUMANSH_FORCE_TRANSLATE_BINDING=") ||
		strings.HasPrefix(line, "export HUMANSH_FORCE_LITERAL_BINDING=") ||
		(strings.HasPrefix(line, "export PATH=") && strings.Contains(line, ".local/bin")) ||
		(strings.HasPrefix(line, "source ") && (strings.Contains(line, "/humansh/shell/zsh/humansh.zsh") || strings.Contains(line, "/humansh/shell/bash/humansh.bash")))
}

func managedBlock(paths Paths, cfg RuntimeConfig) string {
	binDir := filepath.Dir(paths.Binary)
	return managedBlockFor(paths, cfg, !pathContains(os.Getenv("PATH"), binDir))
}

func managedBlockFor(paths Paths, cfg RuntimeConfig, includePath bool) string {
	pathLine := ""
	if includePath {
		pathLine = "export PATH=" + shellQuote(filepath.Dir(paths.Binary)) + ":\"$PATH\"\n"
	}
	layout, _ := installLayout(paths, cfg.Shell.Name, "")
	return managedStart + "\n" + pathLine +
		"export HUMANSH_SMART_ENTER='" + boolDigit(cfg.Shell.SmartEnter) + "'\n" +
		"export HUMANSH_CLEAR_LINE_BINDING='" + cfg.Shell.ClearLineBinding + "'\n" +
		"export HUMANSH_FORCE_TRANSLATE_BINDING='" + cfg.Shell.ForceTranslateBinding + "'\n" +
		"export HUMANSH_FORCE_LITERAL_BINDING='" + cfg.Shell.ForceLiteralBinding + "'\n" +
		// The provider label is exported so the integration never has to spawn
		// humansh while .zshrc is sourced just to render "Translating with X…".
		// It is the baseline only: `classify --zle-status` returns the live label
		// on the smart path, so a provider change is corrected without a re-render.
		"export HUMANSH_PROVIDER_LABEL='" + cfg.Provider.Label() + "'\n" +
		"source " + shellQuote(layout.assetPath) + "\n" + managedEnd + "\n"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func removeManagedBlock(content string) (string, error) {
	if strings.Count(content, managedStart) > 1 || strings.Count(content, managedEnd) > 1 {
		return "", fmt.Errorf("managed shell startup block is duplicated or corrupted; run humansh doctor --fix")
	}
	start, end := strings.Index(content, managedStart), strings.Index(content, managedEnd)
	if start < 0 && end < 0 {
		return content, nil
	}
	if start < 0 || end < start {
		return "", fmt.Errorf("managed shell startup block is corrupted; run humansh doctor --fix")
	}
	end += len(managedEnd)
	block := content[start:end]
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == managedStart || trimmed == managedEnd || managedStartupLine(trimmed) {
			continue
		}
		return "", fmt.Errorf("managed shell startup block contains an unrecognized line; run humansh doctor --fix")
	}
	for end < len(content) && (content[end] == '\r' || content[end] == '\n') {
		end++
	}
	return content[:start] + content[end:], nil
}

func renderInstallState(s InstallState) string {
	if s.Version == 1 {
		return fmt.Sprintf("version = %d\nbinary_path = %s\ninstalled_version = %s\nshell = %s\nprotocol = %s\nshell_asset_path = %s\nshell_asset_sha256 = %s\nstartup_file = %s\nmanaged_block_version = %d\n", s.Version, quote(s.BinaryPath), quote(s.InstalledVersion), quote(s.Shell), quote(s.Protocol), quote(s.ShellAssetPath), quote(s.ShellAssetSHA256), quote(s.StartupFile), s.ManagedBlockVersion)
	}
	states := s.ShellStates()
	names := make([]string, 0, len(states))
	var details strings.Builder
	for _, integration := range states {
		names = append(names, integration.Shell)
		prefix := integration.Shell + "_"
		fmt.Fprintf(&details, "%sprotocol = %s\n%sshell_asset_path = %s\n%sshell_asset_sha256 = %s\n%sstartup_file = %s\n", prefix, quote(integration.Protocol), prefix, quote(integration.ShellAssetPath), prefix, quote(integration.ShellAssetSHA256), prefix, quote(integration.StartupFile))
	}
	return fmt.Sprintf("version = 2\nbinary_path = %s\ninstalled_version = %s\nshells = %s\n%smanaged_block_version = %d\n", quote(s.BinaryPath), quote(s.InstalledVersion), quote(strings.Join(names, ",")), details.String(), s.ManagedBlockVersion)
}

func LoadInstallState(path string) (InstallState, error) {
	if err := requireFileMode(path, 0o600); err != nil {
		return InstallState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return InstallState{}, err
	}
	values, err := parseTOMLSubset(string(data))
	if err != nil {
		return InstallState{}, err
	}
	if err := rejectUnknownKeys(values, []string{"version", "binary_path", "installed_version", "shell", "protocol", "shell_asset_path", "shell_asset_sha256", "startup_file", "shells", "zsh_protocol", "zsh_shell_asset_path", "zsh_shell_asset_sha256", "zsh_startup_file", "bash_protocol", "bash_shell_asset_path", "bash_shell_asset_sha256", "bash_startup_file", "managed_block_version"}); err != nil {
		return InstallState{}, err
	}
	var s InstallState
	if s.Version, err = getInt(values, "version", 0); err != nil {
		return s, err
	}
	if v, ok := values["binary_path"]; ok {
		s.BinaryPath = v.scalar
	}
	if v, ok := values["installed_version"]; ok {
		s.InstalledVersion = v.scalar
	}
	if v, ok := values["shell"]; ok {
		s.Shell = v.scalar
	}
	if v, ok := values["protocol"]; ok {
		s.Protocol = v.scalar
	}
	if v, ok := values["shell_asset_path"]; ok {
		s.ShellAssetPath = v.scalar
	}
	if v, ok := values["shell_asset_sha256"]; ok {
		s.ShellAssetSHA256 = v.scalar
	}
	if v, ok := values["startup_file"]; ok {
		s.StartupFile = v.scalar
	}
	if shellsValue, ok := values["shells"]; ok {
		for _, name := range strings.Split(shellsValue.scalar, ",") {
			prefix := name + "_"
			integration := ShellInstallState{Shell: name}
			if v, exists := values[prefix+"protocol"]; exists {
				integration.Protocol = v.scalar
			}
			if v, exists := values[prefix+"shell_asset_path"]; exists {
				integration.ShellAssetPath = v.scalar
			}
			if v, exists := values[prefix+"shell_asset_sha256"]; exists {
				integration.ShellAssetSHA256 = v.scalar
			}
			if v, exists := values[prefix+"startup_file"]; exists {
				integration.StartupFile = v.scalar
			}
			s.Integrations = append(s.Integrations, integration)
		}
	}
	if s.ManagedBlockVersion, err = getInt(values, "managed_block_version", 0); err != nil {
		return s, err
	}
	if err := validateInstallStateShape(values, s.Version); err != nil {
		return s, err
	}
	if s.Version == 1 && len(s.Integrations) == 0 && s.Shell != "" {
		s.Integrations = []ShellInstallState{{Shell: s.Shell, Protocol: s.Protocol, ShellAssetPath: s.ShellAssetPath, ShellAssetSHA256: s.ShellAssetSHA256, StartupFile: s.StartupFile}}
	}
	s.populateLegacyFields()
	return s, s.Validate()
}

func validateInstallStateShape(values map[string]tomlValue, version int) error {
	legacyKeys := []string{"shell", "protocol", "shell_asset_path", "shell_asset_sha256", "startup_file"}
	v2Keys := []string{"shells", "zsh_protocol", "zsh_shell_asset_path", "zsh_shell_asset_sha256", "zsh_startup_file", "bash_protocol", "bash_shell_asset_path", "bash_shell_asset_sha256", "bash_startup_file"}
	switch version {
	case 1:
		for _, key := range v2Keys {
			if _, exists := values[key]; exists {
				return fmt.Errorf("version 1 install state contains version 2 fields")
			}
		}
	case 2:
		for _, key := range legacyKeys {
			if _, exists := values[key]; exists {
				return fmt.Errorf("version 2 install state contains legacy fields")
			}
		}
		shellsValue, exists := values["shells"]
		if !exists || shellsValue.list != nil {
			return fmt.Errorf("version 2 install state shells must be a string")
		}
		selected := map[string]bool{}
		for _, name := range strings.Split(shellsValue.scalar, ",") {
			selected[name] = true
		}
		for _, id := range []string{"zsh", "bash"} {
			for _, suffix := range []string{"protocol", "shell_asset_path", "shell_asset_sha256", "startup_file"} {
				_, present := values[id+"_"+suffix]
				if present != selected[id] {
					return fmt.Errorf("version 2 install state fields do not match shells")
				}
			}
		}
	}
	return nil
}

func (s InstallState) Validate() error {
	if (s.Version != 1 && s.Version != 2) || s.ManagedBlockVersion != 1 {
		return fmt.Errorf("unsupported install-state version")
	}
	if s.InstalledVersion == "" {
		return fmt.Errorf("install state has an empty installed version")
	}
	if !filepath.IsAbs(s.BinaryPath) {
		return fmt.Errorf("install state binary_path must be an absolute path")
	}
	states := s.ShellStates()
	if len(states) == 0 || len(states) > 2 {
		return fmt.Errorf("install state has no supported shell integrations")
	}
	seen := map[string]bool{}
	for _, integration := range states {
		validShellProtocol := integration.Shell == string(shell.Zsh) && integration.Protocol == protocol.Version || integration.Shell == string(shell.Bash) && integration.Protocol == protocol.ReadlineVersion
		if !validShellProtocol || seen[integration.Shell] {
			return fmt.Errorf("install state has an unsupported or duplicate shell protocol")
		}
		seen[integration.Shell] = true
		for name, value := range map[string]string{"shell_asset_path": integration.ShellAssetPath, "startup_file": integration.StartupFile} {
			if !filepath.IsAbs(value) {
				return fmt.Errorf("install state %s must be an absolute path", name)
			}
		}
		digest, err := hex.DecodeString(integration.ShellAssetSHA256)
		if err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("install state shell asset digest is invalid")
		}
	}
	return nil
}

func (s InstallState) ShellStates() []ShellInstallState {
	if len(s.Integrations) > 0 {
		return append([]ShellInstallState(nil), s.Integrations...)
	}
	if s.Shell == "" {
		return nil
	}
	return []ShellInstallState{{Shell: s.Shell, Protocol: s.Protocol, ShellAssetPath: s.ShellAssetPath, ShellAssetSHA256: s.ShellAssetSHA256, StartupFile: s.StartupFile}}
}

func (s InstallState) ShellIDs() []shell.ID {
	states := s.ShellStates()
	ids := make([]shell.ID, 0, len(states))
	for _, integration := range states {
		ids = append(ids, shell.ID(integration.Shell))
	}
	return ids
}

func (s *InstallState) populateLegacyFields() {
	states := s.ShellStates()
	if len(states) == 0 {
		return
	}
	first := states[0]
	s.Shell, s.Protocol = first.Shell, first.Protocol
	s.ShellAssetPath, s.ShellAssetSHA256, s.StartupFile = first.ShellAssetPath, first.ShellAssetSHA256, first.StartupFile
}

func Doctor(paths Paths, cfg RuntimeConfig, binaryVersion string) []string {
	var issues []string
	if err := cfg.Validate(); err != nil {
		issues = append(issues, "invalid config: "+err.Error())
	}
	state, err := LoadInstallState(paths.InstallState)
	if err != nil {
		issues = append(issues, "install state missing or invalid: run `humansh setup`")
		return issues
	}
	home, _ := os.UserHomeDir()
	if state.BinaryPath != paths.Binary {
		issues = append(issues, "install state contains paths outside the current humansh layout: run `humansh setup --repair`")
		return issues
	}
	if binaryVersion != "" && state.InstalledVersion != binaryVersion {
		issues = append(issues, "install state version differs from the running binary: run `humansh doctor --fix`")
	}
	if info, statErr := os.Lstat(paths.Binary); statErr != nil {
		issues = append(issues, "installed binary is missing: reinstall humansh")
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		issues = append(issues, "installed binary is a symlink or special file: reinstall humansh")
	} else if info.Mode().Perm()&0o111 == 0 {
		issues = append(issues, "installed binary is not executable: run `humansh doctor --fix`")
	}
	for _, integration := range state.ShellStates() {
		id := shell.ID(integration.Shell)
		layout, layoutErr := installLayout(paths, id, home)
		if layoutErr != nil {
			issues = append(issues, layoutErr.Error())
			continue
		}
		if integration.ShellAssetPath != layout.assetPath || integration.StartupFile != layout.startupFile {
			issues = append(issues, "install state contains paths outside the current humansh layout: run `humansh setup --repair`")
			continue
		}
		if integration.Protocol != layout.protocol {
			issues = append(issues, fmt.Sprintf("%s install-state protocol is invalid: run `humansh doctor --fix`", shellDisplay(id)))
		}
		issues = append(issues, doctorShellIntegration(paths, configForShell(cfg, id), layout, integration)...)
	}
	for _, owned := range []struct {
		path string
		mode os.FileMode
	}{{paths.ConfigFile, 0o600}, {paths.ClassifierFile, 0o600}, {paths.Credentials, 0o600}, {paths.InstallState, 0o600}} {
		if info, statErr := os.Lstat(owned.path); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				issues = append(issues, fmt.Sprintf("managed file %s is a symlink or special file: repair it manually", owned.path))
			} else if info.Mode().Perm() != owned.mode {
				issues = append(issues, fmt.Sprintf("unsafe permissions on %s: run `humansh doctor --fix`", owned.path))
			}
		}
	}
	return issues
}

func doctorShellIntegration(paths Paths, cfg RuntimeConfig, layout shellInstallLayout, state ShellInstallState) []string {
	var issues []string
	assetInfo, assetStatErr := os.Lstat(layout.assetPath)
	if assetStatErr != nil {
		issues = append(issues, fmt.Sprintf("%s shell asset missing: run `humansh doctor --fix`", shellDisplay(layout.id)))
	} else if assetInfo.Mode()&os.ModeSymlink != 0 || !assetInfo.Mode().IsRegular() {
		issues = append(issues, fmt.Sprintf("%s shell asset is a symlink or special file: run `humansh doctor --fix`", shellDisplay(layout.id)))
	} else {
		data, readErr := os.ReadFile(layout.assetPath)
		if readErr != nil {
			issues = append(issues, fmt.Sprintf("%s shell asset could not be read: run `humansh doctor --fix`", shellDisplay(layout.id)))
		} else if assetInfo.Mode().Perm() != 0o644 {
			issues = append(issues, fmt.Sprintf("unsafe %s shell asset permissions: run `humansh doctor --fix`", shellDisplay(layout.id)))
		} else if digest := fmt.Sprintf("%x", sha256.Sum256(data)); digest != state.ShellAssetSHA256 || digest != fmt.Sprintf("%x", sha256.Sum256(layout.asset)) {
			issues = append(issues, fmt.Sprintf("%s shell asset hash mismatch: run `humansh doctor --fix`", shellDisplay(layout.id)))
		}
	}
	startup, err := os.ReadFile(layout.startupFile)
	if err != nil || !strings.Contains(string(startup), managedStart) {
		issues = append(issues, fmt.Sprintf("managed %s block missing: run `humansh doctor --fix`", filepath.Base(layout.startupFile)))
		return issues
	}
	if _, blockErr := removeManagedBlock(string(startup)); blockErr != nil {
		issues = append(issues, fmt.Sprintf("managed %s block is corrupted: run `humansh doctor --fix`", filepath.Base(layout.startupFile)))
		return issues
	}
	start := strings.Index(string(startup), managedStart)
	end := strings.Index(string(startup)[start:], managedEnd) + start + len(managedEnd)
	actualBlock := string(startup)[start:end]
	withoutPath := strings.TrimSuffix(managedBlockFor(paths, cfg, false), "\n")
	withPath := strings.TrimSuffix(managedBlockFor(paths, cfg, true), "\n")
	if actualBlock != withoutPath && actualBlock != withPath {
		issues = append(issues, fmt.Sprintf("managed %s block differs from validated configuration: run `humansh doctor --fix`", filepath.Base(layout.startupFile)))
	}
	if layout.id == shell.Zsh {
		if line, found := laterEnterBindingClobber(string(startup), end); found {
			issues = append(issues, fmt.Sprintf("startup line %d contains a bindkey command after humansh that may replace Enter in emacs, viins, or vicmd; move that command before the humansh block, then run `humansh setup --repair`", line))
		}
	}
	if !pathContains(os.Getenv("PATH"), filepath.Dir(paths.Binary)) && !strings.Contains(string(startup), filepath.Dir(paths.Binary)) {
		issues = append(issues, "~/.local/bin is absent from PATH and the managed block: run `humansh doctor --fix`")
	}
	return issues
}

func shellDisplay(id shell.ID) string {
	if id == shell.Bash {
		return "Bash"
	}
	return "Zsh"
}

func laterEnterBindingClobber(content string, blockEnd int) (int, bool) {
	if blockEnd < 0 || blockEnd > len(content) {
		return 0, false
	}
	baseLine := strings.Count(content[:blockEnd], "\n") + 1
	for offset, line := range strings.Split(content[blockEnd:], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || fields[0] != "bindkey" {
			continue
		}
		resetsKeymaps := fields[1] == "-e" || fields[1] == "-v" || fields[1] == "-d" || fields[1] == "-A" || fields[1] == "-R"
		bindsEnter := strings.Contains(trimmed, "^M") || strings.Contains(trimmed, "^J") || strings.Contains(trimmed, `\r`) || strings.Contains(trimmed, `\n`) || strings.Contains(trimmed, `\x0d`) || strings.Contains(trimmed, `\x0a`)
		if resetsKeymaps || bindsEnter {
			return baseLine + offset, true
		}
	}
	return 0, false
}

func RepairPermissions(paths Paths) error {
	if err := ensurePrivateDirectory(paths.ConfigDir); err != nil {
		return err
	}
	for _, owned := range []struct {
		path string
		mode os.FileMode
	}{{paths.ConfigFile, 0o600}, {paths.ClassifierFile, 0o600}, {paths.Credentials, 0o600}, {paths.InstallState, 0o600}, {filepath.Join(paths.ShellDir, "humansh.zsh"), 0o644}, {filepath.Join(paths.BashShellDir, "humansh.bash"), 0o644}, {paths.Binary, 0o755}} {
		if info, err := os.Lstat(owned.path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return fmt.Errorf("refusing to repair permissions through symlink or special file %s", owned.path)
			}
			if err := os.Chmod(owned.path, owned.mode); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func boolDigit(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
func pathContains(pathValue, directory string) bool {
	for _, item := range filepath.SplitList(pathValue) {
		if item == directory {
			return true
		}
	}
	return false
}
