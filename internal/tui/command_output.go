package tui

import (
	"bufio"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const commandOutputLimit = 256 * 1024
const commandOutputTruncated = "[bootrecov: earlier command output truncated]\n"

type boundedCommandOutput struct {
	data      []byte
	truncated bool
}

func (o *boundedCommandOutput) write(fragment []byte) {
	limit := commandOutputLimit - len(commandOutputTruncated)
	if len(fragment) >= limit {
		o.data = append(o.data[:0], fragment[len(fragment)-limit:]...)
		o.truncated = true
		return
	}
	if excess := len(o.data) + len(fragment) - limit; excess > 0 {
		copy(o.data, o.data[excess:])
		o.data = o.data[:len(o.data)-excess]
		o.truncated = true
	}
	o.data = append(o.data, fragment...)
}

func (o *boundedCommandOutput) bytes() []byte {
	if !o.truncated {
		return o.data
	}
	result := make([]byte, 0, len(commandOutputTruncated)+len(o.data))
	result = append(result, commandOutputTruncated...)
	return append(result, o.data...)
}

var commandOutputSink commandOutputBroadcaster
var commandOutputDeliverySlot = make(chan struct{}, 1)

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

func (b *commandOutputBroadcaster) current() func(string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sink
}

func withCommandOutputSink[T any](sink func(string), fn func() T) T {
	restore := commandOutputSink.set(sink)
	defer restore()
	return fn()
}

func drainCommandStream(r io.Reader, consume func([]byte)) error {
	reader := bufio.NewReaderSize(r, 32*1024)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) != 0 {
			consume(fragment)
		}
		if err == nil || errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
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
	sink := commandOutputSink.current()
	var events chan string
	var sinkDone chan struct{}
	if sink != nil {
		// Only one delivery worker may be stuck in a sink at a time.
		// If that happens, later commands still drain their pipes.
		select {
		case commandOutputDeliverySlot <- struct{}{}:
			events = make(chan string, 16)
			sinkDone = make(chan struct{})
			go func() {
				defer close(sinkDone)
				defer func() { <-commandOutputDeliverySlot }()
				for line := range events {
					sink(line)
				}
			}()
		default:
		}
	}
	emit := func(fragment []byte) {
		if events == nil {
			return
		}
		line := strings.TrimSpace(string(fragment))
		if line == "" {
			return
		}
		select {
		case events <- line:
		default:
		}
	}

	var out boundedCommandOutput
	var outMu sync.Mutex
	var wg sync.WaitGroup
	read := func(r io.Reader, readErr *error) {
		defer wg.Done()
		*readErr = drainCommandStream(r, func(fragment []byte) {
			outMu.Lock()
			out.write(fragment)
			outMu.Unlock()
			emit(fragment)
		})
	}
	var stdoutErr, stderrErr error
	wg.Add(2)
	go read(stdout, &stdoutErr)
	go read(stderr, &stderrErr)
	// Wait closes StdoutPipe/StderrPipe. Drain both readers first so a
	// short-lived command cannot lose output before the scanners run.
	wg.Wait()
	waitErr := cmd.Wait()
	if events != nil {
		close(events)
		// UI delivery is best effort. A stopped UI must not hold the
		// command or its pipe readers open indefinitely.
		select {
		case <-sinkDone:
		case <-time.After(25 * time.Millisecond):
		}
	}
	return out.bytes(), errors.Join(waitErr, stdoutErr, stderrErr)
}
