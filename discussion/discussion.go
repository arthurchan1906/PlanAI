package discussion

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"aipmc/ai"
	pmdb "aipmc/db"
	"aipmc/search"
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

// Search queries discussion_log with optional FTS, source/type filters, and pagination.
func Search(client *ai.Client, query, source, typeFilter, projectPath string, page, pageSize int) ([]map[string]any, int, error) {
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

	if query != "" {
		hits := search.FTS5WithDB(db, query, 100)
		if hits != nil {
			var discIDs []string
			for _, h := range hits {
				if h.Type == "discussion" {
					discIDs = append(discIDs, h.ID)
				}
			}
			if len(discIDs) > 0 {
				if client != nil && client.Enabled() {
					if reranked := rerank(client, query, discIDs, db); reranked != nil {
						discIDs = reranked
					}
				}
				total = len(discIDs)
				offset := (page - 1) * pageSize
				end := offset + pageSize
				if end > len(discIDs) {
					end = len(discIDs)
				}
				for _, id := range discIDs[offset:end] {
					out = append(out, getByID(db, id))
				}
				sort.Slice(out, func(i, j int) bool {
					a, _ := time.Parse("2006-01-02T15:04:05", u.Str(out[i]["created_at"]))
					b, _ := time.Parse("2006-01-02T15:04:05", u.Str(out[j]["created_at"]))
					return a.After(b)
				})
				if out == nil {
					out = []map[string]any{}
				}
				return out, total, nil
			}
		}
	}

	where := "WHERE 1=1"
	var args []any
	if source != "" {
		where += " AND source = ?"
		args = append(args, source)
	}
	if query != "" {
		where += " AND content LIKE ?"
		args = append(args, "%"+query+"%")
	}
	if typeFilter != "" {
		where += " AND (" + typeFilterSQL(typeFilter) + ")"
	}
	db.QueryRow("SELECT COUNT(*) FROM discussion_log "+where, args...).Scan(&total)
	offset := (page - 1) * pageSize
	selectArgs := append(args, pageSize, offset)
	rows, err := db.Query("SELECT id, session_id, role, source, content, metadata, created_at FROM discussion_log "+where+" ORDER BY created_at DESC, rowid DESC LIMIT ? OFFSET ?", selectArgs...)
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
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func rerank(client *ai.Client, query string, ids []string, db *sql.DB) []string {
	if client == nil || !client.Enabled() {
		return nil
	}
	vdb, err := pmdb.OpenVectors()
	if err != nil {
		return nil
	}
	defer vdb.Close()

	var texts []string
	var validIDs []string
	for _, id := range ids {
		var embJSON string
		var content string
		if vdb.QueryRow("SELECT embedding_json FROM vectors WHERE id = ?", id).Scan(&embJSON) == nil && embJSON != "" {
			db.QueryRow("SELECT content FROM discussion_log WHERE id = ?", id).Scan(&content)
			if content != "" {
				texts = append(texts, content)
				validIDs = append(validIDs, id)
			}
		}
	}
	if len(texts) == 0 {
		return nil
	}

	allTexts := append([]string{query}, texts...)
	embs, err := client.Embed(allTexts)
	if err != nil || len(embs) < 2 {
		return nil
	}

	type scoreEntry struct {
		id    string
		score float64
	}
	var entries []scoreEntry
	queryEmb := embs[0]
	for i, emb := range embs[1:] {
		entries = append(entries, scoreEntry{validIDs[i], ai.CosineSimilarity(queryEmb, emb)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].score > entries[j].score })
	result := make([]string, len(entries))
	for i, s := range entries {
		result[i] = s.id
	}
	return result
}

func getByID(db *sql.DB, id string) map[string]any {
	var rid, sid, role, src, content, metadata, createdAt string
	row := db.QueryRow("SELECT id, session_id, role, source, content, metadata, created_at FROM discussion_log WHERE id = ?", id)
	if err := row.Scan(&rid, &sid, &role, &src, &content, &metadata, &createdAt); err != nil {
		return map[string]any{"id": id, "content": "(deleted)"}
	}
	return map[string]any{"id": rid, "session_id": sid, "role": role, "source": src, "content": content, "metadata": metadata, "created_at": createdAt}
}
