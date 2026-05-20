package tui

import (
	"bufio"
	"bytes"
	"io"
	"os/exec"
	"strings"
	"sync"
)

var commandOutputSink commandOutputBroadcaster

type commandOutputBroadcaster struct {
	mu   sync.Mutex
	sink func(string)
}

func (b *commandOutputBroadcaster) set(sink func(string)) func() {
	b.mu.Lock()
	previous := b.sink
	b.sink = sink
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		b.sink = previous
		b.mu.Unlock()
	}
}

func (b *commandOutputBroadcaster) emit(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	b.mu.Lock()
	sink := b.sink
	b.mu.Unlock()
	if sink != nil {
		sink(line)
	}
}

func withCommandOutputSink[T any](sink func(string), fn func() T) T {
	restore := commandOutputSink.set(sink)
	defer restore()
	return fn()
}

func runCommandCombinedOutput(cmd *exec.Cmd) ([]byte, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	var outMu sync.Mutex
	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			outMu.Lock()
			out.WriteString(line)
			out.WriteByte('\n')
			outMu.Unlock()
			commandOutputSink.emit(line)
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	waitErr := cmd.Wait()
	wg.Wait()
	return out.Bytes(), waitErr
}
