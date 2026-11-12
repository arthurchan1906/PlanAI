package u

import (
	"testing"
)

const sampleRoutes = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0101A8C0	0003	0	0	0	00000000	0	0	0
`

func TestParseDefaultGateway(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"basic", sampleRoutes, "192.168.1.1"},
		{"no header", "eth0\t00000000\t0101A8C0\t0003\t0\t0\t0\t00000000\n", "192.168.1.1"},
		{"lowest metric wins", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 0200A8C0 0003 0 0 100 00000000 0 0 0
eth0 00000000 0101A8C0 0003 0 0 10 00000000 0 0 0
`, "192.168.1.1"},
		{"prefer eth over vpn", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
tailscale0 00000000 0300A8C0 0003 0 0 0 00000000 0 0 0
eth0 00000000 0101A8C0 0003 0 0 100 00000000 0 0 0
`, "192.168.1.1"},
		{"lowest metric within eth", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth1 00000000 0200A8C0 0003 0 0 10 00000000 0 0 0
eth0 00000000 0101A8C0 0003 0 0 100 00000000 0 0 0
`, "192.168.0.2"},
		{"only vpn falls back to it", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
tailscale0 00000000 0300A8C0 0003 0 0 0 00000000 0 0 0
`, "192.168.0.3"},
		{"ignore lo", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
lo 00000000 0101A8C0 0003 0 0 0 00000000 0 0 0
`, ""},
		{"ignore non-default", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 0000A8C0 0101A8C0 0003 0 0 0 00FFFFFF 0 0 0
`, ""},
		{"ignore non-gateway flag", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 0101A8C0 0001 0 0 0 00000000 0 0 0
`, ""},
		{"zero gateway", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 00000000 0003 0 0 0 00000000 0 0 0
`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDefaultGateway(tt.in); got != tt.want {
				t.Fatalf("parseDefaultGateway() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"localhost": true, "LOCALHOST": true, "127.0.0.1": true,
		"127.1.2.3": true, "::1": true, "10.0.0.1": false, "example.com": false,
	}
	for host, want := range cases {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestHostAddrFromMirrored(t *testing.T) {
	if got := hostAddrFrom("mirrored", []byte(sampleRoutes)); got != "" {
		t.Fatalf("mirrored should skip gateway, got %q", got)
	}
	if got := hostAddrFrom("nat", []byte(sampleRoutes)); got != "192.168.1.1" {
		t.Fatalf("nat gateway = %q", got)
	}
}

func TestWSLHostAddrOverrides(t *testing.T) {
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")
	t.Setenv("AIPMC_WSL_HOST", "172.20.0.1")
	if got := WSLHostAddr(); got != "172.20.0.1" {
		t.Fatalf("explicit host: got %q", got)
	}
	t.Setenv("AIPMC_WSL_HOST", "off")
	if got := WSLHostAddr(); got != "" {
		t.Fatalf("off: got %q", got)
	}
	t.Setenv("AIPMC_WSL_HOST", "172.20.0.1")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "0")
	if got := WSLHostAddr(); got != "" {
		t.Fatalf("kill switch: got %q", got)
	}
}
