package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsDrvFSPath(t *testing.T) {
	cases := map[string]bool{
		"/mnt/c/project": true,
		"/mnt/d":         true,
		"/mnt/":          false,
		"/home/user/x":   false,
		"C:\\project":    false,
	}
	for p, want := range cases {
		if got := isDrvFSPath(p); got != want {
			t.Errorf("isDrvFSPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestBinaryShadowWarning(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := binaryShadowWarning(a, a); got != "" {
		t.Fatalf("same binary should not warn: %q", got)
	}
	if got := binaryShadowWarning(a, b); got == "" {
		t.Fatal("different binaries should warn")
	}
}

func TestWSLDoctorInfo(t *testing.T) {
	t.Setenv("AIPMC_FORCE_WSL", "1")
	t.Setenv("AIPMC_WSL_HOST", "172.20.0.1")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")
	info := wslDoctorInfo()
	if info == nil {
		t.Fatal("expected WSL info")
	}
	if info["host_addr"] != "172.20.0.1" {
		t.Fatalf("host_addr = %v", info["host_addr"])
	}
	if info["fallback_enabled"] != true {
		t.Fatalf("fallback_enabled = %v", info["fallback_enabled"])
	}
}

func TestUpstreamDoctorChecksLocalReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPSTREAM_URL", "http://127.0.0.1:"+port+"/v1")
	t.Setenv("AIPMC_WSL_HOST", "")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "0")

	checks, _ := upstreamDoctorChecks()
	if len(checks) == 0 {
		t.Fatal("expected at least one upstream")
	}
	if checks[0]["local_reachable"] != true {
		t.Fatalf("local_reachable = %v", checks[0]["local_reachable"])
	}
	if checks[0]["effective"] != "local" {
		t.Fatalf("effective = %v", checks[0]["effective"])
	}
}

func TestUpstreamDoctorHintsWhenUnreachable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AIPMC_FORCE_WSL", "1")
	t.Setenv("UPSTREAM_URL", "http://127.0.0.1:1/v1")
	t.Setenv("AIPMC_WSL_HOST", "127.0.0.1")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")

	_, hints := upstreamDoctorChecks()
	if len(hints) == 0 {
		t.Fatal("expected a firewall hint")
	}
	found := false
	for _, h := range hints {
		if strings.Contains(h, "New-NetFirewallRule") {
			found = true
		}
	}
	if !found {
		t.Fatalf("hints = %v", hints)
	}
}

func TestUpstreamDoctorSkipsNonLoopback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPSTREAM_URL", "https://api.example.com/v1")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "0")

	checks, _ := upstreamDoctorChecks()
	if len(checks) == 0 {
		t.Fatal("expected at least one upstream")
	}
	if checks[0]["probe"] != "skipped" {
		t.Fatalf("probe = %v", checks[0]["probe"])
	}
	if checks[0]["effective"] != "direct" {
		t.Fatalf("effective = %v", checks[0]["effective"])
	}
}

func TestDoctorProjectPath(t *testing.T) {
	if got := doctorProjectPath("/tmp/proj/.pmai/data/pmai.db", "/tmp/other"); got != "/tmp/proj" {
		t.Fatalf("db path root = %q", got)
	}
	if got := doctorProjectPath("", "/tmp/other"); got != "/tmp/other" {
		t.Fatalf("cwd fallback = %q", got)
	}
}

func TestProxyDoctorStatus(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	t.Setenv("PROXY_PORT", port)
	got := proxyDoctorStatus()
	if got["reachable"] != true {
		t.Fatalf("reachable = %v", got["reachable"])
	}
}

func TestRunDoctorAddsWSLSections(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME override is not portable on Windows")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("AIPMC_FORCE_WSL", "1")
	t.Setenv("AIPMC_WSL_HOST", "127.0.0.1")
	t.Setenv("UPSTREAM_URL", "http://127.0.0.1:1/v1")
	report := runDoctor("")
	if _, ok := report["wsl"]; !ok {
		t.Fatal("missing wsl section")
	}
	if _, ok := report["upstreams"]; !ok {
		t.Fatal("missing upstreams section")
	}
	if _, ok := report["hints"]; !ok {
		t.Fatal("missing hints section")
	}
}
