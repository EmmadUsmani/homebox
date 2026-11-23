package session

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	outputChunk  = 32 * 1024
	maxTerminals = 12
)

type Terminal struct {
	pool     *Pool
	mu       sync.Mutex
	next     uint64
	closed   bool
	active   int
	sessions map[string]*terminalSession
}

type terminalSession struct {
	ssh     *ssh.Session
	input   io.WriteCloser
	cancel  context.CancelFunc
	release func()
	output  <-chan []byte
}

type OpenResult struct {
	ID         string `json:"id,omitempty"`
	Error      string `json:"error,omitempty"`
	KeyChanged bool   `json:"keyChanged,omitempty"`
}

type Output struct {
	Data  []byte `json:"data,omitempty"`
	Ended bool   `json:"ended,omitempty"`
}

func NewTerminal(pool *Pool) *Terminal {
	return &Terminal{pool: pool, sessions: make(map[string]*terminalSession)}
}

func (t *Terminal) Open(ctx context.Context, ref VMRef, rows, cols int) OpenResult {
	if !validSize(rows, cols) {
		return OpenResult{Error: "Invalid terminal dimensions"}
	}
	t.mu.Lock()
	if t.closed || t.active >= maxTerminals {
		t.mu.Unlock()
		return OpenResult{Error: "No terminal slots available"}
	}
	t.active++
	t.mu.Unlock()

	session, err := openTerminal(ctx, t.pool, ref, rows, cols)
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		t.active--
		var changed *HostKeyChangedError
		if errors.As(err, &changed) {
			return OpenResult{Error: changed.Error(), KeyChanged: true}
		}
		return OpenResult{Error: err.Error()}
	}
	if t.closed || ctx.Err() != nil {
		t.active--
		session.close()
		return OpenResult{Error: "Terminal opening cancelled"}
	}
	t.next++
	id := strconv.FormatUint(t.next, 10)
	t.sessions[id] = session
	return OpenResult{ID: id}
}

func openTerminal(ctx context.Context, pool *Pool, ref VMRef, rows, cols int) (*terminalSession, error) {
	ctx, cancelSetup := context.WithTimeout(ctx, setupTimeout)
	defer cancelSetup()
	client, release, err := pool.Acquire(ctx, ref)
	if err != nil {
		return nil, err
	}
	// An SSH server that stops replying to channel setup can leave a request
	// pending indefinitely. Close the unresponsive transport to unblock it.
	stopSetup := context.AfterFunc(ctx, func() { _ = client.Close() })
	session, err := client.NewSession()
	if err != nil {
		stopSetup()
		release()
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	stream := outputWriter{done: lifetime.Done(), chunks: make(chan []byte, 1)}
	session.Stdout, session.Stderr = stream, stream
	input, err := session.StdinPipe()
	if err == nil {
		err = session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 1})
	}
	if err == nil {
		err = session.Shell()
	}
	if !stopSetup() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		cancel()
		_ = session.Close()
		release()
		return nil, err
	}
	go func() {
		_ = session.Wait()
		close(stream.chunks)
		release()
	}()
	return &terminalSession{ssh: session, input: input, cancel: cancel, release: release, output: stream.chunks}, nil
}

func (s *terminalSession) close() {
	s.cancel()
	_ = s.ssh.Close()
	s.release()
}

type outputWriter struct {
	done   <-chan struct{}
	chunks chan []byte
}

func (w outputWriter) Write(data []byte) (int, error) {
	for written := 0; written < len(data); {
		size := min(outputChunk, len(data)-written)
		select {
		case <-w.done:
			return written, io.ErrClosedPipe
		case w.chunks <- append([]byte(nil), data[written:written+size]...):
			written += size
		}
	}
	return len(data), nil
}

func (t *Terminal) get(id string) *terminalSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessions[id]
}

// Read returns one bounded chunk. The web view requests the next chunk only
// after xterm has parsed this one, propagating back-pressure to the SSH window.
func (t *Terminal) Read(ctx context.Context, id string) Output {
	session := t.get(id)
	if session == nil {
		return Output{Ended: true}
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case data, ok := <-session.output:
		if !ok {
			t.Close(id)
			return Output{Ended: true}
		}
		return Output{Data: data}
	case <-timer.C:
		return Output{}
	case <-ctx.Done():
		return Output{}
	}
}

func (t *Terminal) Write(id, data string) error {
	session := t.get(id)
	if session == nil {
		return io.ErrClosedPipe
	}
	if len(data) > outputChunk {
		return errors.New("terminal input exceeds chunk size")
	}
	_, err := io.WriteString(session.input, data)
	return err
}

func validSize(rows, cols int) bool {
	return rows > 0 && rows <= 1000 && cols > 0 && cols <= 1000
}

func (t *Terminal) Resize(id string, rows, cols int) error {
	if !validSize(rows, cols) {
		return errors.New("invalid terminal dimensions")
	}
	if session := t.get(id); session != nil {
		return session.ssh.WindowChange(rows, cols)
	}
	return io.ErrClosedPipe
}

func (t *Terminal) Close(id string) {
	t.mu.Lock()
	session := t.sessions[id]
	if session != nil {
		delete(t.sessions, id)
		t.active--
	}
	t.mu.Unlock()
	if session != nil {
		session.close()
	}
}

func (t *Terminal) ServiceShutdown() error {
	t.mu.Lock()
	t.closed = true
	sessions := t.sessions
	t.sessions = nil
	t.active -= len(sessions)
	t.mu.Unlock()
	t.pool.Close()
	for _, session := range sessions {
		session.close()
	}
	return nil
}
