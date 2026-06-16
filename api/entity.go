package api

import (
	"fmt"
	"net/http"

	"aipmc/store"
	"aipmc/u"
	"aipmc/web"
)

func handleGetEntity(w http.ResponseWriter, entity, id string) {
	switch entity {
	case "tasks":
		t, err := store.GetTask(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, t)
	case "commits":
		c, err := store.GetCommit(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, c)
	case "plans":
		p, err := store.GetPlan(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, p)
	case "bugs":
		b, err := store.GetBug(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, b)
	case "decisions":
		d, err := store.GetDecision(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, d)
	case "ideas":
		i, err := store.GetIdea(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, i)
	case "roadmaps":
		r, err := store.GetRoadmap(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, r)
	case "principles":
		p, err := store.GetPrinciple(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, p)
	case "visions":
		v, err := store.GetVision(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, v)
	case "threads":
		t, err := store.GetThread(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, t)
	case "agents":
		a, err := store.GetAgentProfile(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, a)
	default:
		web.SendError(w, 404, fmt.Sprintf("unknown entity: %s", entity))
	}
}

func handlePatchEntity(w http.ResponseWriter, entity, id string, body map[string]any) {
	switch entity {
	case "tasks":
		task, err := store.UpdateTask(id, pstr(body, "status", ""), pstr(body, "note", ""), false, false)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, task)
	case "commits":
		c, err := store.UpdateCommit(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, c)
	case "plans":
		p, err := store.UpdatePlan(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, p)
	case "bugs":
		b, err := store.UpdateBug(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, b)
	case "decisions":
		d, err := store.UpdateDecisionStatus(id, pstr(body, "status", ""))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, d)
	case "ideas":
		if _, hasNote := body["note"]; hasNote {
			idea, err := store.ReviewIdea(id, u.Str(body["status"]), u.Str(body["note"]))
			if err != nil {
				web.SendError(w, 400, err.Error())
				return
			}
			web.SendJSON(w, idea)
		} else {
			idea, err := store.UpdateIdea(id, body)
			if err != nil {
				web.SendError(w, 400, err.Error())
				return
			}
			web.SendJSON(w, idea)
		}
	case "roadmaps":
		r, err := store.UpdateRoadmap(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, r)
	case "principles":
		p, err := store.UpdatePrinciple(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, p)
	case "visions":
		v, err := store.UpdateVision(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, v)
	case "threads":
		t, err := store.UpdateThread(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, t)
	case "agents":
		a, err := store.UpdateAgentProfile(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, a)
	default:
		web.SendError(w, 404, fmt.Sprintf("unknown entity: %s", entity))
	}
}
