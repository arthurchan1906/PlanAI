package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
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

// 反馈 #48：库比二进制新时必须硬失败并给可读指引，而不是继续跑到某次
// SELECT/Scan 上抛出「expected 14 destination arguments in Scan, not 13」。
func TestEnsureSchemaIfNeededRejectsNewerSchema(t *testing.T) {
	d := openDBT(t)
	mustExecT(t, d, "PRAGMA user_version = "+strconv.Itoa(SCHEMA_VERSION+1))

	err := EnsureSchemaIfNeeded(d)
	if err == nil {
		t.Fatal("must fail when DB schema is newer than this binary")
	}
	var tooNew *SchemaTooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("want *SchemaTooNewError, got %T: %v", err, err)
	}
	if tooNew.DBVersion != SCHEMA_VERSION+1 || tooNew.BinaryVersion != SCHEMA_VERSION {
		t.Errorf("versions = (%d,%d), want (%d,%d)", tooNew.DBVersion, tooNew.BinaryVersion, SCHEMA_VERSION+1, SCHEMA_VERSION)
	}
	if tableOrVTableExists(d, "tasks") {
		t.Error("DDL must not run against a newer-schema database")
	}
	if !strings.Contains(err.Error(), "升级 aipmc") {
		t.Errorf("error must tell the user to upgrade aipmc, got: %v", err)
	}
}

// openAt 是生产路径（pmdb.Open → openAt）：库比二进制新时 Open 必须直接报错。
func TestOpenAtSurfacesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pmai.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("PRAGMA user_version = " + strconv.Itoa(SCHEMA_VERSION+1)); err != nil {
		t.Fatal(err)
	}
	d.Close()

	if _, err := openAt(path); err == nil {
		t.Fatal("openAt must reject a database migrated by a newer aipmc")
	} else {
		var tooNew *SchemaTooNewError
		if !errors.As(err, &tooNew) {
			t.Fatalf("openAt error type = %T, want *SchemaTooNewError: %v", err, err)
		}
	}
}
