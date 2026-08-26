package db

import (
	"strconv"
	"testing"
)

// TestEnsureSchemaIfNeededSkipsDDLWhenCurrent: user_version 已最新时守卫必须跳过
// 全部 DDL——锁竞争热路径（discussion 写连接/每次 Open）只做一次廉价 PRAGMA 读，
// 不得再触发 43 条写锁 DDL（bug-20260826-164859-0643c5）。
func TestEnsureSchemaIfNeededSkipsDDLWhenCurrent(t *testing.T) {
	d := openDBT(t)
	// 直接标最新版本，但不建任何表：若守卫误跑 DDL，tasks 会被创建出来。
	mustExecT(t, d, "PRAGMA user_version = "+strconv.Itoa(SCHEMA_VERSION))
	if err := EnsureSchemaIfNeeded(d); err != nil {
		t.Fatalf("EnsureSchemaIfNeeded(current): %v", err)
	}
	if tableOrVTableExists(d, "tasks") {
		t.Fatal("DDL ran on an up-to-date database: tasks table exists")
	}
	if tableOrVTableExists(d, "discussion_log") {
		t.Fatal("DDL ran on an up-to-date database: discussion_log table exists")
	}
}

// TestEnsureSchemaIfNeededRunsDDLWhenStale: 旧库（user_version < 当前）必须补跑
// DDL+migrate 并推进 user_version——守卫只在已最新时跳过。
func TestEnsureSchemaIfNeededRunsDDLWhenStale(t *testing.T) {
	d := openDBT(t)
	if err := EnsureSchemaIfNeeded(d); err != nil {
		t.Fatalf("EnsureSchemaIfNeeded(stale): %v", err)
	}
	for _, tbl := range []string{"tasks", "discussion_log", "fts5_index"} {
		if !tableOrVTableExists(d, tbl) {
			t.Errorf("table %s missing after stale guard ran DDL", tbl)
		}
	}
	upToDate, err := schemaUpToDate(d)
	if err != nil {
		t.Fatalf("schemaUpToDate: %v", err)
	}
	if !upToDate {
		t.Fatal("user_version not advanced after EnsureSchemaIfNeeded")
	}
}
