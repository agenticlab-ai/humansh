//go:build darwin || linux

package processrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
)

type Spec struct {
	Path      string
	Args      []string
	Stdin     []byte
	Dir       string
	Env       []string
	MaxStdout int
	MaxStderr int
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type Runner interface {
	Run(context.Context, Spec) (Result, error)
}

type ExecRunner struct{}

var ErrOutputLimit = errors.New("provider output exceeded capture limit")

func (ExecRunner) Run(ctx context.Context, spec Spec) (Result, error) {
	if spec.MaxStdout <= 0 {
		spec.MaxStdout = 1 << 20
	}
	if spec.MaxStderr <= 0 {
		spec.MaxStderr = 1 << 20
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append([]string(nil), spec.Env...)
	cmd.Stdin = bytes.NewReader(spec.Stdin)
	stdout := &boundedBuffer{limit: spec.MaxStdout}
	stderr := &boundedBuffer{limit: spec.MaxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		err = ctx.Err()
	}
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if stdout.overflow || stderr.overflow {
		return result, ErrOutputLimit
	}
	return result, err
}

func IsOutputLimit(err error) bool { return errors.Is(err, ErrOutputLimit) }

type boundedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.overflow = true
		return written, nil
	}
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.buf.Write(p)
	return written, nil
}
func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

func MinimalEnv(tempDir string, extra map[string]string) []string {
	allowed := map[string]struct{}{
		"PATH": {}, "HOME": {}, "LANG": {}, "LC_ALL": {}, "LC_CTYPE": {}, "TZ": {},
		"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "HTTPS_PROXY": {}, "HTTP_PROXY": {}, "ALL_PROXY": {}, "NO_PROXY": {},
		"https_proxy": {}, "http_proxy": {}, "all_proxy": {}, "no_proxy": {}, "CODEX_HOME": {},
	}
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, keep := allowed[key]; keep {
			values[key] = value
		}
	}
	values["TMPDIR"] = tempDir
	for key, value := range extra {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(values))
	for _, key := range keys {
		value := values[key]
		out = append(out, key+"="+value)
	}
	return out
}

func IsNotFound(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return true
	}
	var pathErr *os.PathError
	return errors.As(err, &pathErr) && errors.Is(pathErr.Err, syscall.ENOENT)
}

func CopyLimited(dst io.Writer, src io.Reader, limit int64) error {
	n, err := io.CopyN(dst, src, limit+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n > limit {
		return fmt.Errorf("input exceeds %d bytes", limit)
	}
	return nil
}
