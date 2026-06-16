package executil

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"

	"sparkle-transcoder/internal/priority"

	log "github.com/sirupsen/logrus"
)

const (
	maxCommandStdout = 32 * 1024 * 1024
	maxCommandStderr = 1 * 1024 * 1024
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	RunInDir(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

type LocalRunner struct {
	LowPriority bool
}

func (r LocalRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.RunInDir(ctx, "", name, args...)
}

func (r LocalRunner) RunInDir(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if dir != "" && !filepath.IsAbs(name) && filepath.Base(name) != name {
		if abs, err := filepath.Abs(name); err == nil {
			name = abs
		}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	stdout := newLimitedBuffer(maxCommandStdout)
	stderr := newLimitedBuffer(maxCommandStderr)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmdLabel := commandLabel(cmd)
	log.Debugf("command: %s", cmdLabel)
	if err := cmd.Start(); err != nil {
		return stdout.Bytes(), err
	}
	if r.LowPriority {
		if err := priority.LowPriority(cmd.Process.Pid); err != nil {
			log.Warnf("unable to lower process priority for pid %d: %v", cmd.Process.Pid, err)
		}
	}
	err := cmd.Wait()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return stdout.Bytes(), ctxErr
		}
		if stdout.Truncated() {
			return stdout.Bytes(), fmt.Errorf("%s produced more than %d bytes on stdout", cmd.String(), maxCommandStdout)
		}
		errOutput := stderr.String()
		if errOutput == "" {
			errOutput = string(stdout.Bytes())
		}
		return stdout.Bytes(), fmt.Errorf("%s failed: %w: %s", cmdLabel, err, errOutput)
	}
	if stdout.Truncated() {
		return stdout.Bytes(), fmt.Errorf("%s produced more than %d bytes on stdout", cmd.String(), maxCommandStdout)
	}
	return stdout.Bytes(), nil
}

func commandLabel(cmd *exec.Cmd) string {
	label := cmd.String()
	if cmd.Dir == "" {
		return label
	}
	return fmt.Sprintf("(cd %s && %s)", cmd.Dir, label)
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       []byte
	limit     int
	truncated int64
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	available := b.limit - len(b.buf)
	if available > 0 {
		if available > len(p) {
			available = len(p)
		}
		b.buf = append(b.buf, p[:available]...)
	}
	if available < len(p) {
		b.truncated += int64(len(p) - available)
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, len(b.buf))
	copy(out, b.buf)
	return out
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.truncated == 0 {
		return string(b.buf)
	}
	return fmt.Sprintf("%s\n... truncated %d bytes ...", string(b.buf), b.truncated)
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated > 0
}
