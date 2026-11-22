package egress

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

func (p *Proxy) connect(ctx context.Context, w http.ResponseWriter, pinned string, entry *decision) {
	p.tunnelMu.Lock()
	if p.stopping || len(p.slots) == cap(p.slots) {
		p.tunnelMu.Unlock()
		entry.allowed, entry.reason = false, "tunnel capacity reached"
		http.Error(w, entry.reason, http.StatusServiceUnavailable)
		return
	}
	p.slots <- struct{}{}
	p.tunnels.Add(1)
	p.tunnelMu.Unlock()
	defer p.tunnels.Done()
	defer func() { <-p.slots }()

	upstream, err := p.dialer.DialContext(ctx, "tcp", pinned)
	if err != nil {
		entry.reason = "upstream dial failed"
		http.Error(w, entry.reason, http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	client, buffer, err := http.NewResponseController(w).Hijack()
	if err != nil {
		entry.reason = "client hijack failed"
		http.Error(w, entry.reason, http.StatusInternalServerError)
		return
	}
	defer client.Close()

	stop := context.AfterFunc(p.tunnelContext, func() {
		client.Close()
		upstream.Close()
	})
	defer stop()

	refreshDeadline := func() {
		deadline := time.Now().Add(time.Hour)
		client.SetDeadline(deadline)
		upstream.SetDeadline(deadline)
	}
	refreshDeadline()
	if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err == nil {
		err = buffer.Flush()
	}
	if err != nil {
		entry.reason = "client handshake failed"
		return
	}

	sent := make(chan int64, 1)
	// Drain bytes read along with the CONNECT headers, then read directly from TCP.
	input := io.MultiReader(io.LimitReader(buffer.Reader, int64(buffer.Reader.Buffered())), client)
	go func() { sent <- copyHalf(upstream, input, refreshDeadline) }()
	entry.received = copyHalf(client, upstream, refreshDeadline)
	entry.sent = <-sent
	entry.reason = "tunnel closed"
}

type tunnelWriter struct {
	io.Writer
	refreshDeadline func()
}

func (w tunnelWriter) Write(data []byte) (int, error) {
	n, err := w.Writer.Write(data)
	if n > 0 {
		w.refreshDeadline()
	}
	return n, err
}

func copyHalf(dst net.Conn, src io.Reader, refreshDeadline func()) int64 {
	bytes, err := io.Copy(tunnelWriter{dst, refreshDeadline}, src)
	if half, ok := dst.(interface{ CloseWrite() error }); ok && err == nil {
		half.CloseWrite()
	} else {
		dst.Close()
	}
	return bytes
}

func (p *Proxy) stopTunnels() {
	p.tunnelMu.Lock()
	p.stopping = true
	p.tunnelMu.Unlock()
	p.cancelTunnels()
	p.tunnels.Wait()
}
