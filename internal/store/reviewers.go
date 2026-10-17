package store

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"buyersworkbench/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReviewerInput struct {
	Email, DisplayName, Password string
	ExpiresAt                    time.Time
}
type ReviewerResult struct {
	Created            bool      `json:"created"`
	UserID             string    `json:"user_id"`
	WorkspaceID        string    `json:"workspace_id"`
	ExpiresAt          time.Time `json:"expires_at"`
	MustChangePassword bool      `json:"must_change_password"`
}

// CreateReviewer is a local privileged operation, never a registration route.
// Repeating the same email preserves credentials, expiry, and workspace data.
func CreateReviewer(ctx context.Context, pool *pgxpool.Pool, in ReviewerInput) (ReviewerResult, error) {
	var result ReviewerResult
	email := strings.ToLower(strings.TrimSpace(in.Email))
	address, err := mail.ParseAddress(email)
	name := strings.TrimSpace(in.DisplayName)
	workspaceName := name
	if utf8.RuneCountInString(workspaceName) > 92 {
		workspaceName = string([]rune(workspaceName)[:92])
	}
	if err != nil || address.Address != email || len(email) > 254 || !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return result, errors.New("provide a valid reviewer email and a name of 1 to 100 characters")
	}
	if in.ExpiresAt.IsZero() {
		in.ExpiresAt = time.Now().UTC().Add(14 * 24 * time.Hour)
	}
	if !in.ExpiresAt.After(time.Now()) || in.ExpiresAt.After(time.Now().Add(366*24*time.Hour)) {
		return result, errors.New("reviewer expiry must be within the next year")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(636217210042)`); err != nil {
		return result, err
	}
	var owner string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM users WHERE is_owner AND active AND disabled_at IS NULL`).Scan(&owner); err != nil {
		return result, errors.New("an active owner is required")
	}
	var isOwner bool
	err = tx.QueryRow(ctx, `SELECT u.id::text,u.is_owner,w.id::text,u.expires_at,i.must_change_password
        FROM identities i JOIN users u ON u.id=i.user_id LEFT JOIN workspaces w ON w.owner_user_id=u.id AND w.kind='reviewer'
        WHERE i.provider='local' AND i.issuer='local' AND i.subject=$1`, email).
		Scan(&result.UserID, &isOwner, &result.WorkspaceID, &result.ExpiresAt, &result.MustChangePassword)
	if err == nil {
		if isOwner {
			return result, errors.New("the owner cannot be provisioned as a reviewer")
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errors.New("existing identity cannot be provisioned as a reviewer")
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return result, err
	}
	ids := make([]string, 3)
	for i := range ids {
		ids[i], err = newID()
		if err != nil {
			return result, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO users(id,display_name,expires_at) VALUES($1,$2,$3)`, ids[0], name, in.ExpiresAt); err != nil {
		return result, errors.New("create reviewer account")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO identities(id,user_id,provider,issuer,subject,password_hash,must_change_password) VALUES($1,$2,'local','local',$3,$4,true)`, ids[1], ids[0], email, hash); err != nil {
		return result, errors.New("create reviewer identity")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO workspaces(id,kind,owner_user_id,name,seed_version,business_date) VALUES($1,'reviewer',$2,$3,$4,$5::date)`, ids[2], ids[0], workspaceName+" sandbox", SeedVersion, BusinessDate); err != nil {
		return result, errors.New("create reviewer workspace")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'reviewer')`, ids[2], ids[0]); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome) VALUES(gen_random_uuid(),$1,'reviewer.create','user',$2,'success')`, owner, ids[0]); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return ReviewerResult{true, ids[0], ids[2], in.ExpiresAt, true}, nil
}

func DisableReviewer(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var owner string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM users WHERE is_owner AND active AND disabled_at IS NULL`).Scan(&owner); err != nil {
		return errors.New("an active owner is required")
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET active=false,disabled_at=COALESCE(disabled_at,now()) WHERE id=$1 AND NOT is_owner`, userID)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("reviewer not found")
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome) VALUES(gen_random_uuid(),$1,'reviewer.disable','user',$2,'success')`, owner, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func SetReviewerExpiry(ctx context.Context, pool *pgxpool.Pool, userID string, expires time.Time) error {
	if !expires.After(time.Now()) || expires.After(time.Now().Add(366*24*time.Hour)) {
		return errors.New("reviewer expiry must be within the next year")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var owner string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM users WHERE is_owner AND active AND disabled_at IS NULL`).Scan(&owner); err != nil {
		return errors.New("an active owner is required")
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET expires_at=$2 WHERE id=$1 AND NOT is_owner`, userID, expires)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("reviewer not found")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome) VALUES(gen_random_uuid(),$1,'reviewer.expiry','user',$2,'success')`, owner, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CopyApprovedSnapshots is invoked only by an explicit privileged CLI flag.
// It copies public projections, never private lineage or arbitrary owner notes.
func CopyApprovedSnapshots(ctx context.Context, pool *pgxpool.Pool, target string) error {
	_, err := pool.Exec(ctx, `INSERT INTO wb_snapshot_copies(workspace_id,generation,snapshot_id,snapshot)
 SELECT target.id,target.generation,c.snapshot_id,c.snapshot FROM workspaces target
 CROSS JOIN workspaces source JOIN users owner ON owner.id=source.owner_user_id AND owner.is_owner
 JOIN wb_snapshot_copies c ON c.workspace_id=source.id AND c.generation=source.generation
 WHERE target.id=$1 AND target.kind='reviewer' AND source.kind='presentation'
 ON CONFLICT DO NOTHING`, target)
	return err
}
