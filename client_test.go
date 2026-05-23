package ovnflow

import (
	"context"
	"errors"
	"testing"
)

func TestErrorKindSupportsErrorsIsAndAs(t *testing.T) {
	err := wrap(ErrorAlreadyExists, dbOVNNorthbound, tableLogicalSwitch, "create", "ls-web", "", errors.New("duplicate"))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("errors.Is(%v, ErrAlreadyExists) = false", err)
	}
	if !IsKind(err, ErrorAlreadyExists) {
		t.Fatalf("IsKind(%v, ErrorAlreadyExists) = false", err)
	}

	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("errors.As(%v, *Error) = false", err)
	}
	if typed.Table != tableLogicalSwitch || typed.Object != "ls-web" {
		t.Fatalf("typed error context = %#v", typed)
	}
}

func TestContextErrorClassification(t *testing.T) {
	if got := KindOf(classifyContext(context.Canceled, dbOVNNorthbound, "", "connect", "")); got != ErrorCanceled {
		t.Fatalf("KindOf(canceled) = %s", got)
	}
	if got := KindOf(classifyContext(context.DeadlineExceeded, dbOVNNorthbound, "", "connect", "")); got != ErrorTimeout {
		t.Fatalf("KindOf(deadline) = %s", got)
	}
}

func TestBuilderCannotExecuteTwiceAfterValidation(t *testing.T) {
	builder := (&NBClient{}).LogicalSwitch("ls").Create()
	if !builder.once.mark() {
		t.Fatal("first mark failed")
	}
	if builder.once.mark() {
		t.Fatal("second mark succeeded, want one-shot builder")
	}
}

func TestConfigFromEnvUsesIntegrationVariables(t *testing.T) {
	t.Setenv(EnvOVSAddr, "tcp:127.0.0.1:6640")
	t.Setenv(EnvOVNNBAddr, "tcp:127.0.0.1:6641")
	t.Setenv(EnvOVNSBAddr, "tcp:127.0.0.1:6642")

	cfg := ConfigFromEnv()
	if cfg.OVSAddr != "tcp:127.0.0.1:6640" || cfg.OVNNBAddr != "tcp:127.0.0.1:6641" || cfg.OVNSBAddr != "tcp:127.0.0.1:6642" {
		t.Fatalf("ConfigFromEnv() = %#v", cfg)
	}
}
