// Package agent provides a minimal coding agent with 4 tools:
// read_file, write_file, edit_file, bash.
// Inspired by Pi Agent's philosophy: fewer tools, Bash as universal tool.
package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Tool represents a callable tool registered with the agent.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any // JSON Schema for the tool's parameters
	Exec        func(args map[string]any, workDir string) string
}

// DefaultTools returns the 4-tool MVP set.
func DefaultTools() []Tool {
	return []Tool{
		{
			Name:        "read_file",
			Description: "读取一个文件的内容。用于在修改前理解现有代码。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]string{
						"type":        "string",
						"description": "要读取的文件路径，相对于项目根目录",
					},
				},
				"required": []string{"file_path"},
			},
			Exec: func(args map[string]any, workDir string) string {
				path := getStr(args, "file_path")
				if path == "" {
					return "错误: 缺少 file_path 参数"
				}
				fullPath := resolvePath(workDir, path)
				data, err := os.ReadFile(fullPath)
				if err != nil {
					return fmt.Sprintf("错误: 无法读取文件 — %v", err)
				}
				if len(data) > 8000 {
					data = data[:8000]
					return string(data) + "\n\n... (文件过长，已截断至 8000 字符)"
				}
				return string(data)
			},
		},
		{
			Name:        "write_file",
			Description: "创建或覆盖一个文件。如果父目录不存在会自动创建。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]string{
						"type":        "string",
						"description": "文件路径，相对于项目根目录",
					},
					"content": map[string]string{
						"type":        "string",
						"description": "要写入的文件内容",
					},
				},
				"required": []string{"file_path", "content"},
			},
			Exec: func(args map[string]any, workDir string) string {
				path := getStr(args, "file_path")
				content := getStr(args, "content")
				if path == "" {
					return "错误: 缺少 file_path 参数"
				}
				fullPath := resolvePath(workDir, path)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					return fmt.Sprintf("错误: 无法创建目录 — %v", err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					return fmt.Sprintf("错误: 无法写入文件 — %v", err)
				}
				return "已写入 " + path + fmt.Sprintf(" (%d 字节)", len(content))
			},
		},
		{
			Name:        "edit_file",
			Description: "在文件中做精准文本替换。old_string 必须在文件中恰好出现一次，否则操作会失败。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]string{
						"type":        "string",
						"description": "要编辑的文件路径，相对于项目根目录",
					},
					"old_string": map[string]string{
						"type":        "string",
						"description": "要替换的文本，必须与文件内容完全匹配",
					},
					"new_string": map[string]string{
						"type":        "string",
						"description": "替换后的文本",
					},
				},
				"required": []string{"file_path", "old_string", "new_string"},
			},
			Exec: func(args map[string]any, workDir string) string {
				path := getStr(args, "file_path")
				oldStr := getStr(args, "old_string")
				newStr := getStr(args, "new_string")
				if path == "" || oldStr == "" {
					return "错误: 缺少 file_path 或 old_string 参数"
				}
				fullPath := resolvePath(workDir, path)
				data, err := os.ReadFile(fullPath)
				if err != nil {
					return fmt.Sprintf("错误: 无法读取文件 — %v", err)
				}
				content := string(data)
				count := strings.Count(content, oldStr)
				if count == 0 {
					return fmt.Sprintf("错误: old_string 在文件中未找到。请用 read_file 确认文件内容后重试。")
				}
				if count > 1 {
					return fmt.Sprintf("错误: old_string 在文件中出现了 %d 次。请提供更长的上下文使其唯一匹配。", count)
				}
				newContent := strings.Replace(content, oldStr, newStr, 1)
				if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
					return fmt.Sprintf("错误: 无法写入文件 — %v", err)
				}
				oldLines := strings.Count(oldStr, "\n") + 1
				newLines := strings.Count(newStr, "\n") + 1
				return fmt.Sprintf("已修改 %s (%d 行 → %d 行)", path, oldLines, newLines)
			},
		},
		{
			Name:        "bash",
			Description: "执行 shell 命令。用于运行编译、测试、git 操作、搜索代码等。工作目录为项目根目录。命令有 30 秒超时限制。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]string{
						"type":        "string",
						"description": "要执行的 shell 命令",
					},
				},
				"required": []string{"command"},
			},
			Exec: func(args map[string]any, workDir string) string {
				cmdStr := getStr(args, "command")
				if cmdStr == "" {
					return "错误: 缺少 command 参数"
				}
				shell, shellFlag := detectShell()
				cmd := exec.Command(shell, shellFlag, cmdStr)
				cmd.Dir = workDir
				// 30s timeout
				timer := time.AfterFunc(30*time.Second, func() {
					if cmd.Process != nil {
						cmd.Process.Kill()
					}
				})
				output, err := cmd.CombinedOutput()
				timer.Stop()
				out := string(output)
				if len(out) > 5000 {
					out = out[:5000] + "\n... (输出过长，已截断至 5000 字符)"
				}
				if err != nil {
					if len(out) == 0 {
						return fmt.Sprintf("命令执行失败: %v", err)
					}
					return out + fmt.Sprintf("\n(exit: %v)", err)
				}
				if out == "" {
					return "(命令执行成功，无输出)"
				}
				return out
			},
		},
	}
}

// detectShell returns the shell and flag for running a command string.
func detectShell() (string, string) {
	// Try bash first; fall back to cmd on Windows
	if _, err := exec.LookPath("bash"); err == nil {
		return "bash", "-c"
	}
	if _, err := exec.LookPath("sh"); err == nil {
		return "sh", "-c"
	}
	return "cmd", "/c"
}

// resolvePath resolves a relative path against workDir.
// Rejects paths that attempt to escape workDir via "..".
func resolvePath(workDir, relPath string) string {
	// Clean the path and join with workDir
	cleaned := filepath.Clean(relPath)
	full := filepath.Join(workDir, cleaned)
	// Ensure the result is still under workDir
	absWork, _ := filepath.Abs(workDir)
	absFull, _ := filepath.Abs(full)
	if !strings.HasPrefix(absFull, absWork+string(filepath.Separator)) && absFull != absWork {
		// Path escape attempt — fall back to workDir-relative
		return filepath.Join(workDir, filepath.Base(cleaned))
	}
	return full
}

func getStr(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
