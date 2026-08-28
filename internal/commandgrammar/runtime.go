package commandgrammar

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agenticlab-ai/humansh/internal/processrunner"
)

const (
	defaultHelpTimeout = 300 * time.Millisecond
	defaultHelpOutput  = 64 << 10
)

// RuntimeHelpSource inspects the exact installed executable using fixed
// help-only arguments. It does not cache results across classifications, so a
// changed executable is observed on the next request.
type RuntimeHelpSource struct {
	Runner    processrunner.Runner
	Timeout   time.Duration
	MaxOutput int
}

func NewRuntimeAnalyzer() Analyzer {
	return NewAnalyzer(RuntimeHelpSource{})
}

func (source RuntimeHelpSource) Open(ctx context.Context, ref ExecutableRef) (HelpSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := resolveExecutable(ref)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	mode := info.Mode()
	if !mode.IsRegular() || mode.Perm()&0111 == 0 || mode&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return nil, errors.New("resolved help target is not a regular non-privileged executable")
	}
	tempDir, err := os.MkdirTemp("", "humansh-command-help-")
	if err != nil {
		return nil, err
	}
	for _, child := range []string{"home", "config", "cache", "data", "tmp"} {
		if err := os.Mkdir(filepath.Join(tempDir, child), 0700); err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, err
		}
	}
	return &runtimeHelpSession{
		path:      path,
		tempDir:   tempDir,
		env:       isolatedHelpEnv(tempDir),
		runner:    source.runner(),
		timeout:   source.timeout(),
		maxOutput: source.outputLimit(),
	}, nil
}

func (source RuntimeHelpSource) runner() processrunner.Runner {
	if source.Runner != nil {
		return source.Runner
	}
	return processrunner.ExecRunner{}
}

func (source RuntimeHelpSource) timeout() time.Duration {
	if source.Timeout <= 0 {
		return defaultHelpTimeout
	}
	return source.Timeout
}

func (source RuntimeHelpSource) outputLimit() int {
	if source.MaxOutput <= 0 || source.MaxOutput > maxHelpParseBytes {
		return defaultHelpOutput
	}
	return source.MaxOutput
}

type runtimeHelpSession struct {
	path      string
	tempDir   string
	env       []string
	runner    processrunner.Runner
	timeout   time.Duration
	maxOutput int
}

func (session *runtimeHelpSession) Load(ctx context.Context, prefix []string) HelpResult {
	for _, word := range prefix {
		if !validCommandName(word) {
			return HelpResult{Status: HelpUnavailable}
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, session.timeout)
	defer cancel()
	args := append(append(make([]string, 0, len(prefix)+1), prefix...), "--help")
	result, runErr := session.runner.Run(probeCtx, processrunner.Spec{
		Path:      session.path,
		Args:      args,
		Dir:       session.tempDir,
		Env:       session.env,
		MaxStdout: session.maxOutput,
		MaxStderr: session.maxOutput,
	})
	if probeCtx.Err() != nil || ctx.Err() != nil {
		return HelpResult{Status: HelpUnavailable}
	}
	complete := !processrunner.IsOutputLimit(runErr)
	if runErr != nil && !complete && len(result.Stdout) == 0 && len(result.Stderr) == 0 {
		return HelpResult{Status: HelpUnavailable}
	}
	var exitErr *exec.ExitError
	if runErr != nil && complete && !errors.As(runErr, &exitErr) {
		return HelpResult{Status: HelpUnavailable}
	}
	output := make([]byte, 0, len(result.Stdout)+len(result.Stderr)+1)
	output = append(output, result.Stdout...)
	if len(result.Stdout) > 0 && len(result.Stderr) > 0 {
		output = append(output, '\n')
	}
	output = append(output, result.Stderr...)
	node, err := ParseHelp(output, complete)
	if err != nil {
		return HelpResult{Status: HelpUnparseable}
	}
	return HelpResult{Node: node, Status: HelpOK}
}

func (session *runtimeHelpSession) Close() error {
	if session == nil || session.tempDir == "" {
		return nil
	}
	err := os.RemoveAll(session.tempDir)
	session.tempDir = ""
	return err
}

func resolveExecutable(ref ExecutableRef) (string, error) {
	if ref.Head == "" || strings.ContainsAny(ref.Head, "\x00\r\n") {
		return "", errors.New("invalid executable name")
	}
	path := ref.Path
	var err error
	if path != "" {
		if strings.ContainsAny(path, "\x00\r\n") || !filepath.IsAbs(path) {
			return "", errors.New("resolved executable path must be absolute")
		}
	} else {
		path, err = exec.LookPath(ref.Head)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(path) {
			path, err = filepath.Abs(path)
			if err != nil {
				return "", err
			}
		}
	}
	return filepath.Clean(path), nil
}

func isolatedHelpEnv(tempDir string) []string {
	values := map[string]string{
		"HOME":            filepath.Join(tempDir, "home"),
		"LANG":            "C",
		"LC_ALL":          "C",
		"TERM":            "dumb",
		"NO_COLOR":        "1",
		"CLICOLOR":        "0",
		"PAGER":           "cat",
		"MANPAGER":        "cat",
		"TMPDIR":          filepath.Join(tempDir, "tmp"),
		"XDG_CONFIG_HOME": filepath.Join(tempDir, "config"),
		"XDG_CACHE_HOME":  filepath.Join(tempDir, "cache"),
		"XDG_DATA_HOME":   filepath.Join(tempDir, "data"),
	}
	if path := os.Getenv("PATH"); path != "" {
		values["PATH"] = path
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}
