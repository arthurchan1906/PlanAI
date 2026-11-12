package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aipmc/u"
)

func TestForwardToUpstreamUsesWSLFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Pick a closed port on 127.0.0.1; the first dial must be refused.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, closedPort, _ := net.SplitHostPort(probe.Addr().String())
	probe.Close()

	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")
	t.Setenv("AIPMC_WSL_HOST", "192.0.2.1")

	oldClient := upstreamHTTPClient
	oldCfg := loadCfg()
	defer func() {
		upstreamHTTPClient = oldClient
		storeCfg(oldCfg)
	}()

	base := &http.Transport{Proxy: http.ProxyFromEnvironment}
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srv.Listener.Addr().String())
	}
	upstreamHTTPClient = &http.Client{Transport: u.NewWSLFallbackTransport(base)}
	storeCfg(&proxyCfg{upstreamURL: "http://127.0.0.1:" + closedPort, startTime: time.Now()})

	body, err := forwardToUpstream(
		"chat/completions",
		map[string]any{"model": "test"},
		"", "", "test",
		httptest.NewRequest("POST", "http://example.invalid/", nil),
	)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %s", body)
	}
}
