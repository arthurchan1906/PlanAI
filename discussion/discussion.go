package discussion

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aipmc/ai"
	pmdb "aipmc/db"
	"aipmc/u"
)

// Embed computes embeddings for discussion_log rows and stores them in pmai_vectors.db.
func Embed(client *ai.Client, batchSize int) (int, error) {
	if client == nil || !client.Enabled() {
		return 0, fmt.Errorf("AI not configured")
	}
	db, err := pmdb.Open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	vdb, err := pmdb.OpenVectors()
	if err != nil {
		return 0, err
	}
	defer vdb.Close()

	q := "SELECT id, content FROM discussion_log"
	var args []any
	if batchSize > 0 {
		q += " LIMIT ?"
		args = append(args, batchSize)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type rec struct{ id, content string }
	var batch []rec
	for rows.Next() {
		var r rec
		rows.Scan(&r.id, &r.content)
		if r.content == "" {
			continue
		}
		runes := []rune(r.content)
		if len(runes) < 30 {
			continue
		}
		var exists int
		vdb.QueryRow("SELECT COUNT(*) FROM vectors WHERE id = ?", r.id).Scan(&exists)
		if exists > 0 {
			continue
		}
		text := r.content
		if len(runes) > 3000 {
			text = string(runes[:3000])
		}
		r.content = text
		batch = append(batch, r)
	}
	if len(batch) == 0 {
		return 0, nil
	}

	count := 0
	for i := 0; i < len(batch); i += 10 {
		end := i + 10
		if end > len(batch) {
			end = len(batch)
		}
		texts := make([]string, 0, end-i)
		ids := make([]string, 0, end-i)
		for j := i; j < end; j++ {
			texts = append(texts, batch[j].content)
			ids = append(ids, batch[j].id)
		}
		embs, err := client.Embed(texts)
		if err != nil {
			return count, fmt.Errorf("embed batch %d: %w", i, err)
		}
		for k, emb := range embs {
			vdb.Exec("INSERT OR REPLACE INTO vectors (id, embedding_json) VALUES (?, ?)", ids[k], u.JsonStr(emb))
			count++
		}
	}
	return count, nil
}

// Search queries discussion_log with source/session/type filters, pagination, and optional keyword LIKE match.
func Search(client *ai.Client, query, source, sessionID, typeFilter, projectPath, since string, page, pageSize int) ([]map[string]any, int, error) {
	start := time.Now()
	db, err := openProjectDB(projectPath)
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()

	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	var total int
	var out []map[string]any
	mode := "plain_like"
	gramCount := 0
	defer func() {
		// Key log for later verification of recall behaviour: which search
		// mode ran, how many CJK 2-grams were used, and how many rows matched.
		q := []rune(query)
		if len(q) > 80 {
			q = q[:80]
		}
		u.LogShared("DISC", "search query=%q mode=%s terms=%d grams=%d total=%d took=%s",
			string(q), mode, len(strings.Fields(query)), gramCount, total, time.Since(start).Round(time.Millisecond))
	}()
	fromClause := "FROM discussion_log"
	orderBy := "ORDER BY created_at DESC, rowid DESC"

	where := "WHERE 1=1"
	var args []any
	if source != "" {
		where += " AND source = ?"
		args = append(args, source)
	}
	if sessionID != "" {
		where += " AND session_id = ?"
		args = append(args, sessionID)
	}
	if since != "" {
		where += " AND created_at >= ?"
		args = append(args, since)
	}
	if query != "" {
		terms := splitSearchTerms(query)
		if len(terms) <= 1 {
			term := terms[0]
			grams := cjkBigrams(term)
			if len(grams) >= 2 {
				mode = "cjk_boost"
				gramCount = len(grams)
				// CJK recall boost: exact substring match scores 2, each
				// overlapping 2-gram hit scores 1. Rows with score >= 2
				// qualify (exact match, or >=2 of the query's 2-grams),
				// ranked by score so exact matches lead.
				score := "CASE WHEN content LIKE ? THEN 2 ELSE 0 END"
				likeArgs := []any{"%" + term + "%"}
				for _, g := range grams {
					score += " + CASE WHEN content LIKE ? THEN 1 ELSE 0 END"
					likeArgs = append(likeArgs, "%"+g+"%")
				}
				// Evaluate score exactly once via a CTE so the parameter
				// placeholders are not duplicated in WHERE/ORDER BY.
				fromClause = "FROM (SELECT *, rowid AS _rid, (" + score + ") AS _score FROM discussion_log) AS _t"
				where += " AND _score >= 2"
				// The LIKE placeholders live in the FROM clause (inside the
				// CTE), which precedes the WHERE clause in the SQL text, so
				// they must bind first — prepend them to args, before the
				// source/session/since parameters appended earlier.
				args = append(append([]any{}, likeArgs...), args...)
				// Precision guard (Claude review, 8/17): bigram-only rows
				// (e.g. "行为" and "分析" appearing apart) are noise — put
				// exact-substring rows first via a boolean sort key, so the
				// default limit shows precise hits while recall is kept.
				exactFlag := "(CASE WHEN content LIKE ? THEN 1 ELSE 0 END)"
				// The exact-flag placeholder sits in ORDER BY, after the
				// WHERE-clause filters and before LIMIT/OFFSET.
				args = append(args, "%"+term+"%")
				orderBy = "ORDER BY " + exactFlag + " DESC, _score DESC, created_at DESC, _rid DESC"
			} else {
				where += " AND content LIKE ?"
				args = append(args, "%"+term+"%")
			}
		} else {
			mode = "multi_term"
			var clauses []string
			for _, t := range terms {
				if t == "" {
					continue
				}
				clauses = append(clauses, "content LIKE ?")
				args = append(args, "%"+t+"%")
			}
			if len(clauses) > 0 {
				where += " AND (" + strings.Join(clauses, " OR ") + ")"
			}
		}
	}
	if typeFilter != "" {
		where += " AND (" + typeFilterSQL(typeFilter) + ")"
	}
	db.QueryRow("SELECT COUNT(*) "+fromClause+" "+where, args...).Scan(&total)
	offset := (page - 1) * pageSize
	selectArgs := append(args, pageSize, offset)
	rows, err := db.Query("SELECT id, session_id, role, source, content, metadata, created_at "+fromClause+" "+where+" "+orderBy+" LIMIT ? OFFSET ?", selectArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, sid, role, src, content, metadata, createdAt string
		rows.Scan(&id, &sid, &role, &src, &content, &metadata, &createdAt)
		out = append(out, map[string]any{"id": id, "session_id": sid, "role": role, "source": src, "content": content, "metadata": metadata, "created_at": createdAt})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, total, nil
}

// splitSearchTerms splits a query into individual search terms.
// Uses whitespace as delimiter; supports both CJK and ASCII text.
func splitSearchTerms(query string) []string {
	return strings.Fields(query)
}

// cjkBigrams extracts overlapping 2-character runs from consecutive CJK
// runs in s. For "行为分析" it returns [行为 为分 分析], enabling recall of
// non-contiguous matches like "行为测量分析" (hits 行为 + 分析) that a plain
// LIKE '%行为分析%' would miss.
func cjkBigrams(s string) []string {
	runes := []rune(s)
	var grams []string
	i := 0
	for i < len(runes) {
		if isCJK(runes[i]) {
			j := i
			for j < len(runes) && isCJK(runes[j]) {
				j++
			}
			for k := i; k+1 < j; k++ {
				grams = append(grams, string(runes[k:k+2]))
			}
			i = j
		} else {
			i++
		}
	}
	return grams
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF)
}

func typeFilterSQL(typeFilter string) string {
	toolEmojis := []string{"🔧", "📝", "👁", "🔍", "🆕", "🛠", "📡"}
	typeParts := []string{}
	for _, t := range u.SplitAndTrim(typeFilter, ",") {
		switch t {
		case "user":
			typeParts = append(typeParts, "role = 'user'")
		case "assistant":
			notTool := ""
			for _, e := range toolEmojis {
				if notTool != "" {
					notTool += " AND "
				}
				notTool += "content NOT LIKE '" + e + "%'"
			}
			typeParts = append(typeParts, "(role = 'assistant' AND "+notTool+")")
		case "tool":
			isTool := ""
			for _, e := range toolEmojis {
				if isTool != "" {
					isTool += " OR "
				}
				isTool += "content LIKE '" + e + "%'"
			}
			typeParts = append(typeParts, "("+isTool+")")
		}
	}
	return strings.Join(typeParts, " OR ")
}

func openProjectDB(projectPath string) (*sql.DB, error) {
	if projectPath == "" {
		return pmdb.Open()
	}
	dbPath := filepath.Join(projectPath, ".pmai", "data", "pmai.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("project not found at %s", projectPath)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
