package httpapi

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5"
)

type Workspace struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Kind         string  `json:"kind"`
	Generation   int64   `json:"generation"`
	SeedVersion  *string `json:"seed_version"`
	BusinessDate *string `json:"business_date"`
	DataOrigin   string  `json:"data_origin"`
}

const workspaceProjection = `SELECT w.id::text,w.name,w.kind,w.generation,w.seed_version,to_char(w.business_date,'YYYY-MM-DD'),
	CASE WHEN w.kind='private' THEN 'no_supplier_data' ELSE 'fictional' END
	FROM workspaces w JOIN memberships m ON m.workspace_id=w.id JOIN users u ON u.id=m.user_id
	WHERE m.user_id=$1 AND (w.kind<>'private' OR (w.owner_user_id=$1 AND u.is_owner))`

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func scanWorkspace(row interface{ Scan(...any) error }) (Workspace, error) {
	var w Workspace
	err := row.Scan(&w.ID, &w.Name, &w.Kind, &w.Generation, &w.SeedVersion, &w.BusinessDate, &w.DataOrigin)
	return w, err
}

func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	rows, err := s.pool.Query(r.Context(), workspaceProjection+` ORDER BY CASE WHEN w.kind='presentation' THEN 0 ELSE 1 END,w.name,w.id`, session.user.ID)
	if err != nil {
		fail(w, 503, "Workspaces are temporarily unavailable.")
		return
	}
	defer rows.Close()
	workspaces := make([]Workspace, 0)
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			fail(w, 503, "Workspaces are temporarily unavailable.")
			return
		}
		workspaces = append(workspaces, workspace)
	}
	if rows.Err() != nil {
		fail(w, 503, "Workspaces are temporarily unavailable.")
		return
	}
	respond(w, 200, map[string]any{"workspaces": workspaces})
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		fail(w, 404, "Workspace not found.")
		return
	}
	workspace, err := scanWorkspace(s.pool.QueryRow(r.Context(), workspaceProjection+` AND w.id=$2`, session.user.ID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		s.audit(r.Context(), session.user.ID, "workspace_access", "denied")
		fail(w, 404, "Workspace not found.")
		return
	}
	if err != nil {
		fail(w, 503, "Workspace is temporarily unavailable.")
		return
	}
	respond(w, 200, map[string]any{"workspace": workspace})
}

func (s *Server) foundation(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	respond(w, 200, map[string]string{"milestone": "fictional_workbench", "lenovo": "deferred", "pipeline": "validated_fictional_ingestion", "purchasing_data": "workspace_scoped", "worker": "river_collection_and_durable_schedules"})
}
