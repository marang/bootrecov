package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCommandOutputChild(t *testing.T) {
	mode := os.Getenv("BOOTRECOV_COMMAND_OUTPUT_CHILD")
	if mode == "" {
		return
	}
	chunk := bytes.Repeat([]byte("x"), 32*1024)
	write := func(w *os.File, count int) {
		for range count {
			if _, err := w.Write(chunk); err != nil {
				os.Exit(2)
			}
		}
	}
	switch mode {
	case "long-stdout":
		write(os.Stdout, 64)
		fmt.Fprint(os.Stdout, "stdout-end\n")
	case "long-stderr":
		write(os.Stderr, 64)
		fmt.Fprint(os.Stderr, "stderr-end\n")
	case "mixed":
		write(os.Stdout, 64)
		fmt.Fprint(os.Stderr, "stderr-end\n")
		fmt.Fprint(os.Stdout, "stdout-end\n")
	case "short":
		fmt.Fprint(os.Stdout, "stdout line\n")
		fmt.Fprint(os.Stderr, "stderr line\n")
	}
	os.Exit(0)
}

func runCommandOutputChild(t *testing.T, mode string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCommandOutputChild$")
	cmd.Env = append(os.Environ(), "BOOTRECOV_COMMAND_OUTPUT_CHILD="+mode)
	out, err := runCommandCombinedOutput(cmd)
	if ctx.Err() != nil {
		t.Fatalf("command did not drain and exit before deadline: %v", ctx.Err())
	}
	return out, err
}

func TestRunCommandCombinedOutputDrainsLongStdout(t *testing.T) {
	out, err := runCommandOutputChild(t, "long-stdout")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "stdout-end") {
		t.Fatalf("final output lost; retained %d bytes", len(out))
	}
}

func TestRunCommandCombinedOutputBoundsLongStderrAndKeepsEnd(t *testing.T) {
	out, err := runCommandOutputChild(t, "long-stderr")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 256*1024 {
		t.Fatalf("retained %d bytes of command output", len(out))
	}
	if !strings.Contains(string(out), "stderr-end") {
		t.Fatalf("final stderr lost; retained %d bytes", len(out))
	}
}

func TestRunCommandCombinedOutputDrainsMixedStreams(t *testing.T) {
	out, err := runCommandOutputChild(t, "mixed")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "stdout-end") || !strings.Contains(string(out), "stderr-end") {
		t.Fatalf("mixed output lost an end marker: %q", out)
	}
}

func TestRunCommandCombinedOutputPreservesShortOutput(t *testing.T) {
	out, err := runCommandOutputChild(t, "short")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stdout line\n", "stderr line\n"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("short command output missing %q: %q", want, out)
		}
	}
	if len(out) != len("stdout line\nstderr line\n") {
		t.Fatalf("short command output changed: %q", out)
	}
}

func TestRunCommandCombinedOutputCompletesWithBlockedSink(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCommandOutputChild$")
	cmd.Env = append(os.Environ(), "BOOTRECOV_COMMAND_OUTPUT_CHILD=long-stdout")
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		done <- withCommandOutputSink(func(string) { <-release }, func() result {
			out, err := runCommandCombinedOutput(cmd)
			return result{out, err}
		})
	}()
	select {
	case got := <-done:
		if got.err != nil || ctx.Err() != nil {
			t.Fatalf("command failed while sink was blocked: command=%v context=%v", got.err, ctx.Err())
		}
		if !strings.Contains(string(got.out), "stdout-end") {
			t.Fatalf("final output lost; retained %d bytes", len(got.out))
		}
	case <-ctx.Done():
		t.Fatal("blocked sink prevented command completion")
	}
}

type commandOutputFailingReader struct{ err error }

func (r commandOutputFailingReader) Read([]byte) (int, error) { return 0, r.err }

func TestDrainCommandStreamReportsReaderError(t *testing.T) {
	wantErr := errors.New("read failed")
	var got bytes.Buffer
	err := drainCommandStream(io.MultiReader(strings.NewReader("before"), commandOutputFailingReader{wantErr}), func(fragment []byte) {
		got.Write(fragment)
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("reader error lost: %v", err)
	}
	if got.String() != "before" {
		t.Fatalf("output before reader error lost: %q", got.String())
	}
}
