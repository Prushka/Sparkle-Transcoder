package executil

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"sparkle-transcoder/internal/priority"

	log "github.com/sirupsen/logrus"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type LocalRunner struct {
	LowPriority bool
}

func (r LocalRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	log.Debugf("command: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		return out.Bytes(), err
	}
	if r.LowPriority {
		if err := priority.LowPriority(cmd.Process.Pid); err != nil {
			log.Warnf("unable to lower process priority for pid %d: %v", cmd.Process.Pid, err)
		}
	}
	err := cmd.Wait()
	if err != nil {
		return out.Bytes(), fmt.Errorf("%s failed: %w: %s", cmd.String(), err, out.String())
	}
	return out.Bytes(), nil
}
