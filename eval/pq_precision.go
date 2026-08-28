package eval

// 机制 1 验收：read_discussions 附带相关实体精度重测。
//
// 协议（docs/agent-collab-v1.14-consensus.md 附录 A）：
//   样本 = 30 条真实实质讨论；双标 = 两个独立 agent 各自提取实体引用比对；
//   precision = 被提取且真实存在于库的引用 / 全部被提取引用，≥90%。
//
// 本命令输出「最近 N 条实质讨论中、含实体引用的最后 ≤30 条」的逐行提取结果，
// 供两个 agent 独立复核：每个 agent 应对 Sample[].Refs 做人工/独立判断，
// 比对与 `aipmc eval precision` 的 regex 提取是否一致，进而校验存在性。
// 注意：双标要求「独立 agent 各自提取」，因此复跑本命令（同一确定性 regex）
// 不算双标——必须由第二个 agent 独立标注 Sample[].Refs。

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"aipmc/store"
)

// PrecisionSampleRow 一条含引用的样本行：row_id + 该行 regex 提取出的实体引用。
type PrecisionSampleRow struct {
	RowID string   `json:"row_id"`
	Refs  []string `json:"refs"`
}

// PrecisionReport 机制 1 精度重测报告（JSON + 人类可读双输出）。
type PrecisionReport struct {
	Scanned      int                  `json:"scanned"`
	RowsWithRefs int                  `json:"rows_with_refs"`
	AllRowsRefs  int                  `json:"all_rows_refs"`
	DistinctIDs  int                  `json:"distinct_ids_in_window"`
	SampleRows   int                  `json:"sample_rows"`
	Sample       []PrecisionSampleRow `json:"sample"`
	Extracted    int                  `json:"extracted"`
	Found        int                  `json:"found"`
	PrecisionPct float64              `json:"precision_pct"`
	QueryError   string               `json:"query_error,omitempty"`
	MissingIDs   []string             `json:"missing_ids"`
	GeneratedAt  time.Time            `json:"generated_at"`
}

// BuildPrecisionReport 扫描最近 lastN 条实质讨论，收集含实体引用的行，
// 取时间正序（最新）的最后 maxSample 条作为样本，统计 regex 提取+存在性精度。
// db 用于 FetchRelatedEntities 存在性校验；ReadDiscussions 内部自行开库读取。
func BuildPrecisionReport(db *sql.DB, lastN, maxSample int) (*PrecisionReport, error) {
	if lastN <= 0 {
		lastN = 1000
	}
	if maxSample <= 0 {
		maxSample = 30
	}
	rows, err := store.ReadDiscussions(store.ReadDiscussionsOpts{LastN: lastN})
	if err != nil {
		return nil, fmt.Errorf("read discussions: %w", err)
	}

	// 收集含引用的行（ReadDiscussions 返回时间正序：旧→新，末尾为最新）。
	var refRows []map[string]any
	totalExtracted := []string{}
	rowsWithRefs := 0
	for _, r := range rows {
		c, _ := r["content"].(string)
		ids := store.ExtractRelatedEntities([]string{c})
		if len(ids) > 0 {
			rowsWithRefs++
			refRows = append(refRows, r)
			totalExtracted = append(totalExtracted, ids...)
		}
	}

	// 去重全窗口实体 ID 数
	dedup := map[string]bool{}
	for _, id := range totalExtracted {
		dedup[id] = true
	}

	// 样本 = 最近（末尾）maxSample 条含引用的行
	sampleRows := refRows
	if len(sampleRows) > maxSample {
		sampleRows = sampleRows[len(sampleRows)-maxSample:]
	}
	sample := make([]PrecisionSampleRow, 0, len(sampleRows))
	contents := make([]string, 0, len(sampleRows))
	for _, r := range sampleRows {
		id, _ := r["id"].(string)
		c, _ := r["content"].(string)
		refs := store.ExtractRelatedEntities([]string{c})
		sample = append(sample, PrecisionSampleRow{RowID: id, Refs: refs})
		contents = append(contents, c)
	}

	extracted := store.ExtractRelatedEntities(contents)
	found, qerr := store.FetchRelatedEntities(db, extracted)
	denom := len(extracted)
	foundExact := len(found)
	prec := 0.0
	if denom > 0 {
		prec = float64(foundExact) / float64(denom) * 100
	}

	missing := []string{}
	if qerr == nil {
		exists := map[string]bool{}
		for _, f := range found {
			exists[f.ID] = true
		}
		for _, id := range extracted {
			if !exists[id] {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
	}

	rep := &PrecisionReport{
		Scanned:      len(rows),
		RowsWithRefs: rowsWithRefs,
		AllRowsRefs:  len(totalExtracted),
		DistinctIDs:  len(dedup),
		SampleRows:   len(sample),
		Sample:       sample,
		Extracted:    denom,
		Found:        foundExact,
		PrecisionPct: prec,
		MissingIDs:   missing,
		GeneratedAt:  time.Now(),
	}
	if qerr != nil {
		rep.QueryError = qerr.Error()
	}
	return rep, nil
}

// FormatPrecisionHuman 人类可读输出，对齐 metrics.go printRow 风格。
func FormatPrecisionHuman(rep *PrecisionReport) string {
	var b []byte
	b = append(b, []byte(fmt.Sprintf(
		"scanned=%d rows_with_refs=%d all_rows_refs=%d distinct_ids=%d\n",
		rep.Scanned, rep.RowsWithRefs, rep.AllRowsRefs, rep.DistinctIDs))...)
	b = append(b, []byte(fmt.Sprintf(
		"sample_rows=%d extracted=%d found=%d precision=%.1f%% query_error=%s\n",
		rep.SampleRows, rep.Extracted, rep.Found, rep.PrecisionPct, rep.QueryError))...)
	if len(rep.MissingIDs) > 0 {
		b = append(b, []byte(fmt.Sprintf("missing=%v\n", rep.MissingIDs))...)
	} else {
		b = append(b, []byte("missing=[]\n")...)
	}
	for _, s := range rep.Sample {
		b = append(b, []byte(fmt.Sprintf("  %s refs=%v\n", s.RowID, s.Refs))...)
	}
	return string(b)
}
