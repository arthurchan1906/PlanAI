package main

// ---- Audit Log ----

func recordAudit(actorType, actorID, action, entityType, entityID, summary string) {
	db, err := openDB()
	if err != nil {
		return
	}
	defer db.Close()
	id := slug("audit")
	now := nowISO()
	db.Exec("INSERT INTO audit_log (id, actor_type, actor_id, action, entity_type, entity_id, summary, detail_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, '{}', ?)",
		id, actorType, actorID, action, entityType, entityID, summary, now)
}

func listAuditLog(actorType, entityType string, limit int) ([]map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT id, actor_type, actor_id, action, entity_type, entity_id, summary, created_at FROM audit_log WHERE 1=1"
	var args []any
	if actorType != "" {
		q += " AND actor_type = ?"
		args = append(args, actorType)
	}
	if entityType != "" {
		q += " AND entity_type = ?"
		args = append(args, entityType)
	}
	q += " ORDER BY created_at DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		e := map[string]any{}
		var aid, at, act, et, eid, sum, ca string
		rows.Scan(&aid, &at, &act, &et, &eid, &sum, &ca)
		e["id"] = aid; e["actor_type"] = at; e["action"] = act; e["entity_type"] = et; e["entity_id"] = eid; e["summary"] = sum; e["created_at"] = ca
		result = append(result, e)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}
