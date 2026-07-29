package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/web"
)

func proxyPIDPath(port int) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc", fmt.Sprintf("proxy-%d.pid", port))
}

func (s *Server) handleProxyStop(w http.ResponseWriter, body map[string]any) {
	gcfg := pmdb.LoadGlobalConfig()
	pidPath := proxyPIDPath(gcfg.ProxyPort)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		web.SendError(w, 404, "Proxy 未运行 (PID 文件不存在)")
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		web.SendError(w, 500, "PID 文件格式错误")
		return
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
	} else {
		cmd = exec.Command("kill", "-9", strconv.Itoa(pid))
	}
	if err := cmd.Run(); err != nil {
		web.SendError(w, 500, fmt.Sprintf("停止 Proxy 失败: %v", err))
		return
	}
	os.Remove(pidPath)
	web.SendJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleProxyRestart(w http.ResponseWriter, body map[string]any) {
	gcfg := pmdb.LoadGlobalConfig()

	pidPath := proxyPIDPath(gcfg.ProxyPort)
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if runtime.GOOS == "windows" {
				exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
			} else {
				exec.Command("kill", "-9", strconv.Itoa(pid)).Run()
			}
			os.Remove(pidPath)
		}
	}
	time.Sleep(300 * time.Millisecond)

	exe, _ := os.Executable()
	cmd := exec.Command(exe, "proxy")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// If credentials are unlocked in this session, pass the master password
	// so the proxy subprocess can decrypt without interactive prompt.
	if store := pmdb.GetCredentialStore(); store != nil {
		if pw := store.SessionPassword(); len(pw) > 0 {
			cmd.Env = append(os.Environ(), "AIPMC_MASTER_PASSWORD="+string(pw))
		}
	}
	if err := cmd.Start(); err != nil {
		web.SendError(w, 500, fmt.Sprintf("启动 Proxy 失败: %v", err))
		return
	}

	// Write Codex proxy profile so `codex -p proxy` works immediately
	cfg := pmdb.LoadConfig()
	rt, _ := pmdb.ResolveAgentConfig("codex", gcfg, cfg)
	codexModel := rt.Model
	if codexModel == "" {
		codexModel = "gpt-5.1"
	}
	effort := rt.ReasoningEffort
	if effort == "" {
		effort = "medium"
	}
	codexWriteProxyProfile(fmt.Sprintf("http://127.0.0.1:%d", gcfg.ProxyPort), codexModel, effort)

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", gcfg.ProxyPort)
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		resp, err := http.Get(fmt.Sprintf("http://%s/__proxy/status", proxyAddr))
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			web.SendJSON(w, map[string]any{"ok": true, "port": gcfg.ProxyPort})
			return
		}
	}

	web.SendError(w, 500, "Proxy 6s 内未就绪，请检查日志")
}
