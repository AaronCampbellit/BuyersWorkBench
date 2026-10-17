package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"buyersworkbench/internal/workbench"
)

type pageCursor struct {
	Workspace  string `json:"w"`
	Generation int64  `json:"g"`
	View       string `json:"v"`
	Filter     string `json:"f"`
	After      string `json:"a"`
}

func sealCursor(cursor pageCursor, session string) string {
	data, _ := json.Marshal(cursor)
	mac := hmac.New(sha256.New, []byte(session))
	mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func openCursor(value, session string) (pageCursor, error) {
	var cursor pageCursor
	if len(value) > 2048 {
		return cursor, workbench.ErrInvalid
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return cursor, workbench.ErrInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return cursor, workbench.ErrInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return cursor, workbench.ErrInvalid
	}
	mac := hmac.New(sha256.New, []byte(session))
	mac.Write(data)
	if !hmac.Equal(mac.Sum(nil), signature) || json.Unmarshal(data, &cursor) != nil {
		return cursor, workbench.ErrInvalid
	}
	return cursor, nil
}

type pageItem struct {
	id    string
	value any
}

func (s *Server) pagedWorkbenchList(w http.ResponseWriter, r *http.Request, session *authenticatedSession, result workbench.Overview, view string) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			s.domainError(w, workbench.ErrInvalid)
			return
		}
		limit = parsed
	}
	state, severity, supplier := r.URL.Query().Get("state"), r.URL.Query().Get("severity"), r.URL.Query().Get("supplier")
	if (state != "" && state != "all" && state != "active" && state != "resolved") || (severity != "" && severity != "high" && severity != "medium" && severity != "info") || len(supplier) > 240 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	filter := state + "\x00" + severity + "\x00" + supplier
	items := []pageItem{}
	switch view {
	case "priorities":
		for _, v := range result.Priorities {
			if state == "active" && !v.Active || state == "resolved" && v.Active || severity != "" && v.Severity != severity || supplier != "" && v.SupplierAlias != supplier {
				continue
			}
			items = append(items, pageItem{v.ID, v})
		}
	case "orders":
		for _, v := range result.Orders {
			if supplier == "" || supplier == v.SupplierAlias {
				items = append(items, pageItem{v.ID, v})
			}
		}
	case "suppliers":
		for _, v := range result.Suppliers {
			items = append(items, pageItem{v.Alias, v})
		}
	case "connections":
		for _, v := range result.Connections {
			items = append(items, pageItem{v.ID, v})
		}
	case "notifications":
		for _, v := range result.Notifications {
			items = append(items, pageItem{v.ID, v})
		}
	case "runs":
		for _, v := range result.Runs {
			items = append(items, pageItem{v.ID, v})
		}
	default:
		s.domainError(w, errors.New("unknown list view"))
		return
	}
	start := 0
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err := openCursor(raw, session.token)
		if err != nil {
			s.domainError(w, err)
			return
		}
		if cursor.Workspace != result.WorkspaceID || cursor.View != view || cursor.Filter != filter {
			s.domainError(w, workbench.ErrInvalid)
			return
		}
		if cursor.Generation != result.Generation {
			s.domainError(w, workbench.ErrStale)
			return
		}
		found := false
		for i, item := range items {
			if item.id == cursor.After {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			s.domainError(w, workbench.ErrStale)
			return
		}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	values := make([]any, 0, end-start)
	for _, item := range items[start:end] {
		values = append(values, item.value)
	}
	var next *string
	if end < len(items) {
		token := sealCursor(pageCursor{result.WorkspaceID, result.Generation, view, filter, items[end-1].id}, session.token)
		next = &token
	}
	windowLimit := 500
	windowSize := len(result.Orders)
	switch view {
	case "priorities":
		windowSize = len(result.Priorities)
	case "runs":
		windowLimit = 20
		windowSize = len(result.Runs)
	case "notifications":
		windowLimit = 100
		windowSize = len(result.Notifications)
	case "connections":
		windowLimit = 50
		windowSize = len(result.Connections)
	case "suppliers":
		windowLimit = 500
		windowSize = len(result.Suppliers)
	}
	respond(w, 200, map[string]any{view: values, "generation": result.Generation, "origin": result.Origin, "next_cursor": next, "limit": limit, "window": map[string]any{"scope": "current_generation_bounded_window", "maximum_records": windowLimit, "may_have_older_records": windowSize >= windowLimit}})
}
