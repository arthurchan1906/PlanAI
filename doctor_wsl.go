package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/u"
)

func wslDoctorInfo() map[string]any {
	if !u.IsWSL2() {
		return nil
	}
	host := u.WSLHostAddr()
	return map[string]any{
		"distro":           os.Getenv("WSL_DISTRO_NAME"),
		"kernel":           u.KernelRelease(),
		"networking_mode":  u.WSLNetworkingMode(),
		"host_addr":        host,
		"fallback_enabled": host != "",
	}
}

func isDrvFSPath(path string) bool {
	p := filepath.ToSlash(path)
	return strings.HasPrefix(p, "/mnt/") && len(strings.TrimPrefix(p, "/mnt/")) > 0
}

// doctorProjectPath derives the project root from the database path so
// running `aipmc doctor` from a subdirectory reports the project, not the cwd.
func doctorProjectPath(dbPath, cwd string) string {
	if dbPath != "" {
		return pmdb.ProjectRoot(dbPath)
	}
	return cwd
}

func binaryShadowWarning(executable, pathLookup string) string {
	if executable == "" || pathLookup == "" {
		return ""
	}
	exe, err1 := filepath.EvalSymlinks(executable)
	look, err2 := filepath.EvalSymlinks(pathLookup)
	if err1 != nil || err2 != nil {
		return ""
	}
	if filepath.Clean(exe) == filepath.Clean(look) {
		return ""
	}
	return fmt.Sprintf("PATH 中的 aipmc (%s) 与当前运行的二进制 (%s) 不是同一个；可能是旧版 PyPI 包抢占，执行 which -a aipmc 并卸载旧版", look, exe)
}

type upstreamSource struct {
	Source string
	URL    string
}

func collectUpstreamURLs() []upstreamSource {
	out := []upstreamSource{}
	seen := map[string]bool{}
	add := func(source, raw string) {
		raw = strings.TrimRight(strings.TrimSpace(raw), "/")
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		out = append(out, upstreamSource{Source: source, URL: raw})
	}
	gcfg := pmdb.LoadGlobalConfig()
	global := strings.TrimSpace(gcfg.UpstreamURL)
	if global == "" {
		global = "http://localhost:8080/v1"
	}
	add("global", global)
	reg := pmdb.LoadModelRegistry()
	for _, p := range reg.Providers {
		add("provider:"+p.Name+":openai", p.OpenAIURL)
		add("provider:"+p.Name+":anthropic", p.AnthropicURL)
		add("provider:"+p.Name+":responses", p.ResponsesURL)
	}
	return out
}

func probeTCP(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func urlHostPort(raw string) (host, port string, ok bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", "", false
	}
	host = parsed.Hostname()
	port = parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return host, port, true
}

func upstreamDoctorChecks() ([]map[string]any, []string) {
	checks := []map[string]any{}
	hints := []string{}
	if u.IsWSL2() && u.WSLNetworkingMode() != "mirrored" {
		hints = append(hints, "如果使用 Windows 11 22H2+，可考虑在 .wslconfig 设置 networkingMode=mirrored 后执行 wsl --shutdown；localhost 将双向直通，可能不需要回退。")
	}
	hostAddr := u.WSLHostAddr()
	localCache := map[string]bool{}
	hostCache := map[string]bool{}
	probeLocal := func(addr string) bool {
		if v, ok := localCache[addr]; ok {
			return v
		}
		v := probeTCP(addr, 400*time.Millisecond)
		localCache[addr] = v
		return v
	}
	probeHost := func(addr string) bool {
		if v, ok := hostCache[addr]; ok {
			return v
		}
		v := probeTCP(addr, 400*time.Millisecond)
		hostCache[addr] = v
		return v
	}
	for _, src := range collectUpstreamURLs() {
		host, port, ok := urlHostPort(src.URL)
		if !ok {
			checks = append(checks, map[string]any{
				"source": src.Source, "url": src.URL, "effective": "invalid",
			})
			continue
		}
		entry := map[string]any{
			"source": src.Source,
			"url":    src.URL,
		}
		// Cloud/non-loopback endpoints are reached through HTTP_PROXY in
		// corporate environments; a raw TCP probe would produce false
		// negatives and is irrelevant to the WSL loopback fallback.
		if !u.IsLoopbackHost(host) {
			entry["effective"] = "direct"
			entry["probe"] = "skipped"
			checks = append(checks, entry)
			continue
		}
		addr := net.JoinHostPort(host, port)
		local := probeLocal(addr)
		entry["local_reachable"] = local
		hostReachable := false
		if hostAddr != "" {
			hostReachable = probeHost(net.JoinHostPort(hostAddr, port))
		}
		entry["host_reachable"] = hostReachable
		switch {
		case local:
			entry["effective"] = "local"
		case hostReachable:
			entry["effective"] = "host"
		default:
			entry["effective"] = "unreachable"
			if u.IsWSL2() {
				hints = append(hints, fmt.Sprintf(
					"上游 %s 在 WSL 本地和宿主机 %s:%s 均不可达。宿主机启动模型时加 --host 0.0.0.0，并放行防火墙：New-NetFirewallRule -DisplayName \"aipmc model upstream (WSL)\" -Direction Inbound -Protocol TCP -LocalPort %s -Action Allow -Profile Any",
					src.URL, hostAddr, port, port))
			} else {
				hints = append(hints, fmt.Sprintf("上游 %s 不可达；检查模型服务是否启动、地址/端口是否正确。", src.URL))
			}
		}
		checks = append(checks, entry)
	}
	return checks, dedupeStrings(hints)
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func proxyDoctorStatus() map[string]any {
	gcfg := pmdb.LoadGlobalConfig()
	addr := fmt.Sprintf("127.0.0.1:%d", gcfg.ProxyPort)
	return map[string]any{
		"url":       fmt.Sprintf("http://%s/__proxy/status", addr),
		"reachable": probeTCP(addr, 300*time.Millisecond),
	}
}
