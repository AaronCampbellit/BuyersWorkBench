package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"buyersworkbench/internal/auth"
	"buyersworkbench/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SeedVersion = "fictional-foundation-v1"
const BusinessDate = "2026-10-19"

type OwnerInput struct {
	Email       string
	DisplayName string
	Password    string
}

// BootstrapResult contains only safe IDs and metadata; never credentials.
type BootstrapResult struct {
	Created                 bool   `json:"created"`
	OwnerID                 string `json:"owner_id"`
	PrivateWorkspaceID      string `json:"private_workspace_id"`
	PresentationWorkspaceID string `json:"presentation_workspace_id"`
	SeedVersion             string `json:"seed_version"`
	BusinessDate            string `json:"business_date"`
}

// BootstrapOwner creates the single owner and two empty workspaces atomically.
// A rerun with the same normalized email never changes credentials or profile.
// Order fixtures belong to M1; a fictional clock does not imply collected data.
func BootstrapOwner(ctx context.Context, pool *pgxpool.Pool, input OwnerInput) (BootstrapResult, error) {
	result := BootstrapResult{}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return result, errors.New("provide a valid owner email address")
	}
	if err := migrations.Check(ctx, pool); err != nil {
		return result, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, fmt.Errorf("begin owner bootstrap: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(636217210042)); err != nil {
		return result, err
	}
	var ownerID, existingEmail string
	err = tx.QueryRow(ctx, `SELECT u.id::text, COALESCE(i.subject,'') FROM users u
		LEFT JOIN identities i ON i.user_id=u.id AND i.provider='local' AND i.issuer='local'
		WHERE u.is_owner=true FOR UPDATE OF u`).Scan(&ownerID, &existingEmail)
	if err == nil {
		if existingEmail != email {
			return result, errors.New("an owner already exists; bootstrap cannot create a second owner or replace credentials")
		}
		result.OwnerID = ownerID
		err = tx.QueryRow(ctx, `SELECT p.id::text, d.id::text, d.seed_version, to_char(d.business_date,'YYYY-MM-DD')
			FROM workspaces p JOIN workspaces d ON d.owner_user_id=p.owner_user_id AND d.kind='presentation'
			WHERE p.owner_user_id=$1 AND p.kind='private'`, ownerID).Scan(&result.PrivateWorkspaceID, &result.PresentationWorkspaceID, &result.SeedVersion, &result.BusinessDate)
		if err != nil {
			return result, errors.New("owner foundation is incomplete; inspect the database before recovery")
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, fmt.Errorf("inspect owner: %w", err)
	}
	name := strings.TrimSpace(input.DisplayName)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return result, errors.New("owner name must contain 1 to 100 characters")
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		return result, err
	}
	ids := make([]string, 5)
	for i := range ids {
		ids[i], err = newID()
		if err != nil {
			return result, err
		}
	}
	result = BootstrapResult{true, ids[0], ids[2], ids[3], SeedVersion, BusinessDate}
	if _, err = tx.Exec(ctx, "INSERT INTO users(id,display_name,is_owner) VALUES($1,$2,true)", ids[0], name); err != nil {
		return result, fmt.Errorf("create owner: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO identities(id,user_id,provider,issuer,subject,password_hash)
		VALUES($1,$2,'local','local',$3,$4)`, ids[1], ids[0], email, hash); err != nil {
		return result, fmt.Errorf("create local identity: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO workspaces(id,kind,owner_user_id,name,seed_version,business_date) VALUES
		($1,'private',$3,'Private workspace',NULL,NULL),
		($2,'presentation',$3,'Fictional presentation',$4,$5::date)`, ids[2], ids[3], ids[0], SeedVersion, BusinessDate); err != nil {
		return result, fmt.Errorf("create workspaces: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$3,'owner'),($2,$3,'owner')`, ids[2], ids[3], ids[0]); err != nil {
		return result, fmt.Errorf("create memberships: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome)
		VALUES($1,$2,'owner.bootstrap','user',$2,'success')`, ids[4], ids[0]); err != nil {
		return result, fmt.Errorf("audit bootstrap: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return BootstrapResult{}, fmt.Errorf("commit bootstrap: %w", err)
	}
	return result, nil
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("cannot generate identifier")
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}

type WorkspaceStatus struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	Generation   int64   `json:"generation"`
	SeedVersion  *string `json:"seed_version"`
	BusinessDate *string `json:"business_date"`
}

type FoundationStatus struct {
	SchemaReady       bool              `json:"schema_ready"`
	QueueSchemaReady  bool              `json:"queue_schema_ready"`
	OwnerBootstrapped bool              `json:"owner_bootstrapped"`
	Workspaces        []WorkspaceStatus `json:"workspaces"`
}

func Status(ctx context.Context, pool *pgxpool.Pool) (FoundationStatus, error) {
	result := FoundationStatus{Workspaces: []WorkspaceStatus{}}
	if err := migrations.Check(ctx, pool); err != nil {
		return result, err
	}
	result.SchemaReady = true
	result.QueueSchemaReady = CheckQueue(ctx, pool) == nil
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE is_owner=true)").Scan(&result.OwnerBootstrapped); err != nil {
		return result, err
	}
	rows, err := pool.Query(ctx, "SELECT id::text,kind,name,generation,seed_version,to_char(business_date,'YYYY-MM-DD') FROM workspaces ORDER BY kind,id")
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var workspace WorkspaceStatus
		if err := rows.Scan(&workspace.ID, &workspace.Kind, &workspace.Name, &workspace.Generation, &workspace.SeedVersion, &workspace.BusinessDate); err != nil {
			return result, err
		}
		result.Workspaces = append(result.Workspaces, workspace)
	}
	return result, rows.Err()
}
