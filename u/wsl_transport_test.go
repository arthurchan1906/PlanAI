package u

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestIsConnectRefused(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	if !isConnectRefused(refused) {
		t.Fatal("ECONNREFUSED should trigger fallback")
	}
	notAssigned := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.EADDRNOTAVAIL)}
	if !isConnectRefused(notAssigned) {
		t.Fatal("EADDRNOTAVAIL should trigger fallback")
	}
	if isConnectRefused(context.DeadlineExceeded) {
		t.Fatal("deadline must not trigger fallback")
	}
	if isConnectRefused(errors.New("boom")) {
		t.Fatal("unknown error must not trigger fallback")
	}
}

func TestWSLDialerFallsBackOnRefused(t *testing.T) {
	var got []string
	d := &wslDialer{
		host: func() string { return "172.20.0.1" },
		dialFirst: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, "first:"+addr)
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
		},
		dialFallback: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, "fallback:"+addr)
			c1, c2 := net.Pipe()
			go c2.Close()
			return c1, nil
		},
		logf: func(string, ...any) {},
	}
	conn, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	want := []string{"first:127.0.0.1:8080", "fallback:172.20.0.1:8080"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dial order = %v, want %v", got, want)
	}
}

func TestWSLDialerDoesNotFallbackOnTimeout(t *testing.T) {
	var got []string
	wantErr := context.DeadlineExceeded
	d := &wslDialer{
		host: func() string { return "172.20.0.1" },
		dialFirst: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, "first:"+addr)
			return nil, wantErr
		},
		dialFallback: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, "fallback:"+addr)
			return nil, nil
		},
		logf: func(string, ...any) {},
	}
	if _, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:8080"); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected fallback: %v", got)
	}
}

func TestWSLDialerSkipsNonLoopback(t *testing.T) {
	// This also covers the HTTP proxy case: when a request is proxied,
	// Transport.DialContext receives the proxy address (non-loopback) and
	// must not trigger the WSL fallback.
	var got []string
	d := &wslDialer{
		host: func() string { return "172.20.0.1" },
		dialFallback: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, addr)
			c1, c2 := net.Pipe()
			go c2.Close()
			return c1, nil
		},
		logf: func(string, ...any) {},
	}
	conn, err := d.DialContext(context.Background(), "tcp", "10.0.0.5:3128")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if len(got) != 1 || got[0] != "10.0.0.5:3128" {
		t.Fatalf("got %v", got)
	}
}

func TestModelHTTPClientFallsBackEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Pick a port that is closed on 127.0.0.1 so the first dial is refused.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(probe.Addr().String())
	probe.Close()

	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")
	t.Setenv("AIPMC_WSL_HOST", "192.0.2.1")

	// The fallback dialer ignores the gateway address and connects to the
	// test server, proving the transport switched addresses after refusal.
	base := &http.Transport{Proxy: http.ProxyFromEnvironment}
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srv.Listener.Addr().String())
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: NewWSLFallbackTransport(base)}
	resp, err := client.Get("http://127.0.0.1:" + port + "/")
	if err != nil {
		t.Fatalf("fallback request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}
