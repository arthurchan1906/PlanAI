package main

// ---- Discussion Log ----

func logDiscussion(sessionID, role, source, content string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	id := slug("disc")
	now := nowISO()
	preview := content
	if len([]rune(preview)) > 80 {
		preview = string([]rune(preview)[:80])
	}
	sid := sessionID
	if sid == "" { sid = "unknown" }
	_, err = db.Exec("INSERT INTO discussion_log (id, session_id, role, source, content, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, sid, role, source, content, now)
	if err != nil {
		return nil, err
	}
	syncFTS5Entity(db, "discussion", id, "["+role+"]["+source+"] "+preview, content)
	return map[string]any{"id": id, "status": "created"}, nil
}

func searchDiscussions(query, source string, page, pageSize int) ([]map[string]any, int, error) {
	db, err := openDB()
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()

	if pageSize <= 0 { pageSize = 20 }
	if page <= 0 { page = 1 }
	offset := (page - 1) * pageSize

	// Build query
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

	// Count
	var total int
	db.QueryRow("SELECT COUNT(*) FROM discussion_log "+where, args...).Scan(&total)

	// Select
	selectArgs := append(args, pageSize, offset)
	rows, err := db.Query("SELECT id, session_id, role, source, content, created_at FROM discussion_log "+where+" ORDER BY created_at DESC LIMIT ? OFFSET ?", selectArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, sid, role, src, content, createdAt string
		rows.Scan(&id, &sid, &role, &src, &content, &createdAt)
		out = append(out, map[string]any{
			"id": id, "session_id": sid, "role": role, "source": src,
			"content": content, "created_at": createdAt,
		})
	}
	if out == nil { out = []map[string]any{} }
	return out, total, nil
}

func listDiscussionSources() ([]string, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT DISTINCT source FROM discussion_log WHERE source != '' ORDER BY source")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		result = append(result, s)
	}
	if result == nil { result = []string{} }
	return result, nil
}
