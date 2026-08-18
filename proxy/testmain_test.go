package proxy

import (
	"os"
	"path/filepath"
	"testing"

	pmdb "aipmc/db"
)

// TestMain 隔离测试进程的观测层写入（8/18 M1 对账实测根因）：
// 此前 proxy 包测试直接调用 InjectSessionContext / handler()，会：
//   1. 通过 u.LogShared 写入生产日志 ~/.aipmc/logs/aipmc.log（:148 分母与
//      write_err 被测试噪音污染，eval 对账 reconcile 系统性虚低）；
//   2. 继承 CWD 的 repo .pmai（handler 测试未设 PMAI_HOME），把测试注入写成
//      真实表行，污染 M1 injected 分子。
// 这里统一重定向：AIPMC_LOG=off（u/log.go 支持）+ PMAI_HOME=临时目录。
// 单测自身的 t.Setenv("PMAI_HOME", ...) 会临时覆盖，不受影响。
func TestMain(m *testing.M) {
	os.Setenv("AIPMC_LOG", "off")
	os.Setenv("PMAI_HOME", filepath.Join(os.TempDir(), "aipmc-proxy-test"))
	if _, err := pmdb.Bootstrap(); err != nil {
		// 尽力建临时库，让 handler 测试的注入写临时库而非生产库；失败可忽略
	}
	os.Exit(m.Run())
}
