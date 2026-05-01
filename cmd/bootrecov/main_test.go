package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/marang/bootrecov/internal/tui"
)

func TestHookBackupNowSkipsInsufficientSpaceErrors(t *testing.T) {
	oldCreate := createBootBackupNow
	createBootBackupNow = func() (tui.BootBackup, error) {
		return tui.BootBackup{}, fmt.Errorf("%w: test", tui.ErrInsufficientSnapshotSpace)
	}
	t.Cleanup(func() { createBootBackupNow = oldCreate })
	t.Setenv(riskAcceptEnv, "1")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"hook", "backup-now"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook backup-now should skip insufficient space errors, got %v", err)
	}
}

func TestHookBackupNowReturnsNonSpaceErrors(t *testing.T) {
	expected := errors.New("permission denied")
	oldCreate := createBootBackupNow
	createBootBackupNow = func() (tui.BootBackup, error) {
		return tui.BootBackup{}, expected
	}
	t.Cleanup(func() { createBootBackupNow = oldCreate })
	t.Setenv(riskAcceptEnv, "1")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"hook", "backup-now"})
	if err := cmd.Execute(); !errors.Is(err, expected) {
		t.Fatalf("hook backup-now should return non-space errors, got %v", err)
	}
}

func TestRiskConfirmationAcceptedUsesDefaultNo(t *testing.T) {
	for _, input := range []string{"", "\n", "n", "N", "no", "anything else"} {
		if riskConfirmationAccepted(input) {
			t.Fatalf("expected %q to reject risk acknowledgement", input)
		}
	}
}

func TestRiskConfirmationAcceptedAllowsYes(t *testing.T) {
	for _, input := range []string{"y", "Y", "yes", "YES", " yes \n"} {
		if !riskConfirmationAccepted(input) {
			t.Fatalf("expected %q to accept risk acknowledgement", input)
		}
	}
}

func TestRenderRiskAcknowledgementPromptLooksLikePanel(t *testing.T) {
	prompt := renderRiskAcknowledgementPrompt()
	for _, want := range []string{"Bootrecov risk acknowledgement", "Continue? [y/N]", "╭", "╰"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, strings.Join([]string{"I", "UNDERSTAND"}, " ")) {
		t.Fatalf("prompt should not use the old phrase: %s", prompt)
	}
}
