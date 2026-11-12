package u

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const wslRouteFile = "/proc/net/route"

var (
	wslModeOnce sync.Once
	wslMode     string
)

// IsWSL2 reports whether the process runs inside a WSL2 distro. WSL1 is
// deliberately excluded: it shares the Windows network namespace, so
// localhost already reaches the host.
func IsWSL2() bool {
	if os.Getenv("AIPMC_FORCE_WSL") == "1" {
		return true
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	return strings.Contains(strings.ToLower(KernelRelease()), "microsoft-standard")
}

// KernelRelease returns /proc/sys/kernel/osrelease, or "" when unavailable.
func KernelRelease() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// WSLNetworkingMode returns "nat", "mirrored", or "" when unknown.
func WSLNetworkingMode() string {
	if !IsWSL2() {
		return ""
	}
	wslModeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		out, err := exec.CommandContext(ctx, "wslinfo", "--networking-mode").Output()
		if err != nil {
			return
		}
		m := strings.ToLower(strings.TrimSpace(string(out)))
		if m == "nat" || m == "mirrored" {
			wslMode = m
		}
	})
	return wslMode
}

// WSLHostAddr returns the address WSL should use to reach the Windows host.
// Priority: AIPMC_WSL_HOST_FALLBACK=0 > AIPMC_WSL_HOST explicit >
// WSL2 detection > mirrored skip > /proc/net/route default gateway.
func WSLHostAddr() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AIPMC_WSL_HOST_FALLBACK"))) {
	case "0", "off", "false", "no":
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("AIPMC_WSL_HOST")); v != "" {
		switch strings.ToLower(v) {
		case "auto":
			// fall through to auto detection
		case "off", "none", "0":
			return ""
		default:
			return v
		}
	}
	if !IsWSL2() {
		return ""
	}
	b, err := os.ReadFile(wslRouteFile)
	if err != nil {
		return ""
	}
	return hostAddrFrom(WSLNetworkingMode(), b)
}

// hostAddrFrom applies the networking-mode rule before parsing routes.
func hostAddrFrom(mode string, routes []byte) string {
	if mode == "mirrored" {
		return ""
	}
	return parseDefaultGateway(string(routes))
}

// IsLoopbackHost reports whether host names a loopback endpoint.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// parseDefaultGateway extracts the IPv4 default gateway from /proc/net/route.
// It prefers eth* interfaces because VPN interfaces (tailscale0, tun0, ...)
// often carry a lower metric but route to the VPN gateway, not the Windows
// host. Among the preferred interfaces, the lowest metric wins.
func parseDefaultGateway(routes string) string {
	type candidate struct {
		iface  string
		ip     string
		metric int
	}
	var candidates []candidate
	sc := bufio.NewScanner(strings.NewReader(routes))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 8 || fields[0] == "Iface" {
			continue
		}
		iface, dest, gate, flags := fields[0], fields[1], fields[2], fields[3]
		if iface == "lo" || dest != "00000000" {
			continue
		}
		fl, err := strconv.ParseUint(flags, 16, 32)
		if err != nil || fl&0x0003 != 0x0003 {
			continue
		}
		metric := 0
		if m, err := strconv.Atoi(fields[6]); err == nil {
			metric = m
		}
		ip := hexLEToIPv4(gate)
		if ip == "" {
			continue
		}
		candidates = append(candidates, candidate{iface: iface, ip: ip, metric: metric})
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ei := strings.HasPrefix(candidates[i].iface, "eth")
		ej := strings.HasPrefix(candidates[j].iface, "eth")
		if ei != ej {
			return ei
		}
		return candidates[i].metric < candidates[j].metric
	})
	return candidates[0].ip
}

// hexLEToIPv4 converts the little-endian hex gateway field from
// /proc/net/route (e.g. 0101A8C0) to dotted IPv4 (192.168.1.1).
func hexLEToIPv4(s string) string {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 16, 32)
	if err != nil {
		return ""
	}
	ip := net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	if ip.Equal(net.IPv4zero) {
		return ""
	}
	return ip.String()
}
