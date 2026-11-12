package u

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"
)

// NewModelHTTPClient returns an HTTP client for model upstream calls.
// timeout=0 disables the client-wide timeout (needed for streaming).
func NewModelHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: NewWSLFallbackTransport(nil),
	}
}

// NewWSLFallbackTransport clones base and installs a DialContext that retries
// loopback targets on the WSL host gateway when the local port is refused.
func NewWSLFallbackTransport(base *http.Transport) *http.Transport {
	if base == nil {
		if t, ok := http.DefaultTransport.(*http.Transport); ok {
			base = t.Clone()
		} else {
			base = &http.Transport{Proxy: http.ProxyFromEnvironment}
		}
	} else {
		base = base.Clone()
	}
	fallbackDial := base.DialContext
	if fallbackDial == nil {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		fallbackDial = d.DialContext
	}
	firstDial := (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext
	d := &wslDialer{
		host:         WSLHostAddr,
		dialFirst:    firstDial,
		dialFallback: fallbackDial,
		logf:         log.Printf,
	}
	base.DialContext = d.DialContext
	return base
}

type wslDialer struct {
	host         func() string
	dialFirst    func(ctx context.Context, network, addr string) (net.Conn, error)
	dialFallback func(ctx context.Context, network, addr string) (net.Conn, error)
	logf         func(format string, args ...any)

	mu     sync.Mutex
	logged map[string]bool
}

func (d *wslDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	fallback := d.dialFallback
	if fallback == nil {
		var dialer net.Dialer
		fallback = dialer.DialContext
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || !IsLoopbackHost(host) {
		return fallback(ctx, network, addr)
	}
	if d.host == nil {
		return fallback(ctx, network, addr)
	}
	gw := d.host()
	if gw == "" {
		return fallback(ctx, network, addr)
	}
	first := d.dialFirst
	if first == nil {
		first = fallback
	}
	conn, firstErr := first(ctx, network, addr)
	if firstErr == nil {
		return conn, nil
	}
	if !isConnectRefused(firstErr) {
		return nil, firstErr
	}
	target := net.JoinHostPort(gw, port)
	d.logOnce(addr, target)
	return fallback(ctx, network, target)
}

func (d *wslDialer) logOnce(from, to string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.logged == nil {
		d.logged = make(map[string]bool)
	}
	if d.logged[from] {
		return
	}
	d.logged[from] = true
	if d.logf != nil {
		d.logf("[WSL] %s unreachable locally, falling back to host %s", from, to)
	}
}

// isConnectRefused reports whether err is a dial failure where retrying a
// different address is safe. Timeouts and cancellations are deliberately
// excluded: the local service may simply be slow.
func isConnectRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EADDRNOTAVAIL) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return isConnectRefused(opErr.Err)
	}
	var sysErr *os.SyscallError
	if errors.As(err, &sysErr) {
		return isConnectRefused(sysErr.Err)
	}
	return false
}
