package db

import (
	"os"
	"path/filepath"
	"testing"
)

// A 方案（D 线决策）：同路径 Shared 复用同一连接（进程内单例连接池）。
func TestSharedReusesConnection(t *testing.T) {
	ResetSharedForTest()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".pmai", "data", "pmai.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	// 直接经 sharedAt 验证缓存语义（避免 FindPath 依赖 cwd）。
	d1, err := sharedAt(dbPath)
	if err != nil {
		t.Fatalf("sharedAt 1: %v", err)
	}
	d2, err := sharedAt(dbPath)
	if err != nil {
		t.Fatalf("sharedAt 2: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("同路径 Shared 返回不同连接（未复用）")
	}
	// 复用连接必须可执行查询（schema 就绪）。
	var v int
	if err := d1.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("共享连接查询失败: %v", err)
	}
	ResetSharedForTest()
}

// A 方案：不同 dbPath 返回不同连接（测试隔离天然满足）。
func TestSharedSeparatePaths(t *testing.T) {
	ResetSharedForTest()
	mk := func(t *testing.T) string {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, ".pmai", "data", "pmai.db")
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dbPath, nil, 0644); err != nil {
			t.Fatal(err)
		}
		return dbPath
	}
	p1, p2 := mk(t), mk(t)
	d1, err := sharedAt(p1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := sharedAt(p2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Fatalf("不同 dbPath 返回同一连接（缓存 key 错误）")
	}
	ResetSharedForTest()
}
