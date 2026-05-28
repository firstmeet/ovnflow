//go:build linux

package sdwanlinux

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/firstmeet/ovnflow/v2"
)

type SystemExecutor struct{}

func (SystemExecutor) Run(ctx context.Context, cmd Command) error {
	if strings.TrimSpace(cmd.Program) == "" {
		return &ovnflow.Error{Kind: ovnflow.ErrorValidation, Operation: "exec", Object: cmd.Program, Message: "program must not be empty"}
	}
	execCmd := exec.CommandContext(ctx, cmd.Program, cmd.Args...)
	var stderr bytes.Buffer
	execCmd.Stderr = &stderr
	if err := execCmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if cmd.IgnoreNotFound && commandNotFound(message) {
			return nil
		}
		if cmd.IgnoreAlreadyExists && commandAlreadyExists(message) {
			return nil
		}
		return &ovnflow.Error{Kind: errorKindForExec(err), Operation: "exec", Object: cmd.Program, Message: message, Err: err}
	}
	return nil
}

func commandNotFound(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "no such file") ||
		strings.Contains(message, "not found") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "cannot find device") ||
		strings.Contains(message, "no such process") ||
		strings.Contains(message, "no such table")
}

func commandAlreadyExists(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "file exists") ||
		strings.Contains(message, "already exists")
}

func errorKindForExec(err error) ovnflow.ErrorKind {
	switch {
	case errors.Is(err, context.Canceled):
		return ovnflow.ErrorCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ovnflow.ErrorTimeout
	default:
		return ovnflow.ErrorUnavailable
	}
}
