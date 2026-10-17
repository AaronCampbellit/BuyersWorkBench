package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"time"

	"buyersworkbench/internal/auth"
	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
)

func runWorkbench(ctx context.Context, args []string, out io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "seed", "create-reviewer", "disable-reviewer", "reset-reviewer-password", "set-reviewer-expiry":
	default:
		return false, nil
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var email, name, passwordFile, expires, workspace, userID string
	var includeSnapshots bool
	flags.StringVar(&email, "email", "", "reviewer email")
	flags.StringVar(&name, "name", "", "display name")
	flags.StringVar(&passwordFile, "password-file", "", "restricted temporary password file")
	flags.StringVar(&expires, "expires", "", "RFC3339 expiry, defaults to 14 days")
	flags.StringVar(&workspace, "workspace", "", "workspace UUID")
	flags.StringVar(&userID, "user-id", "", "reviewer UUID")
	flags.BoolVar(&includeSnapshots, "include-approved-snapshots", false, "explicitly clone approved presentation snapshots")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return true, errors.New("invalid arguments; use bwb --help")
	}
	pool, err := store.Open(ctx)
	if err != nil {
		return true, err
	}
	defer pool.Close()
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	switch args[0] {
	case "seed":
		var actor string
		var generation int64
		if err = pool.QueryRow(ctx, `SELECT owner_user_id::text,generation FROM workspaces WHERE id=$1 AND kind<>'private'`, workspace).Scan(&actor, &generation); err != nil {
			return true, errors.New("fictional workspace not found")
		}
		result, err := workbench.New(pool).Seed(ctx, actor, workspace, generation)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(result)
	case "create-reviewer":
		password, err := readPasswordFile(passwordFile)
		if err != nil {
			return true, err
		}
		defer clear(password)
		in := store.ReviewerInput{Email: email, DisplayName: name, Password: string(password)}
		if expires != "" {
			in.ExpiresAt, err = time.Parse(time.RFC3339, expires)
			if err != nil {
				return true, errors.New("expiry must be an RFC3339 timestamp")
			}
		}
		result, err := store.CreateReviewer(ctx, pool, in)
		if err != nil {
			return true, err
		}
		var generation int64
		if err = pool.QueryRow(ctx, `SELECT generation FROM workspaces WHERE id=$1`, result.WorkspaceID).Scan(&generation); err != nil {
			return true, err
		}
		if _, err = workbench.New(pool).Seed(ctx, result.UserID, result.WorkspaceID, generation); err != nil {
			return true, errors.New("reviewer created; seed failed, rerun create-reviewer to resume")
		}
		if includeSnapshots {
			if err = store.CopyApprovedSnapshots(ctx, pool, result.WorkspaceID); err != nil {
				return true, err
			}
		}
		return true, encoder.Encode(result)
	case "set-reviewer-expiry":
		date, err := time.Parse(time.RFC3339, expires)
		if err != nil {
			return true, errors.New("expiry must be an RFC3339 timestamp")
		}
		if err = store.SetReviewerExpiry(ctx, pool, userID, date); err != nil {
			return true, err
		}
		return true, encoder.Encode(map[string]any{"user_id": userID, "expires_at": date})
	case "disable-reviewer":
		if err = store.DisableReviewer(ctx, pool, userID); err != nil {
			return true, err
		}
		return true, encoder.Encode(map[string]bool{"disabled": true, "sessions_revoked": true})
	case "reset-reviewer-password":
		password, err := readPasswordFile(passwordFile)
		if err != nil {
			return true, err
		}
		defer clear(password)
		hash, err := auth.HashPassword(string(password))
		if err != nil {
			return true, err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return true, err
		}
		defer tx.Rollback(context.Background())
		tag, err := tx.Exec(ctx, `UPDATE identities SET password_hash=$2,must_change_password=true WHERE user_id=$1 AND provider='local' AND issuer='local' AND EXISTS(SELECT 1 FROM users WHERE id=$1 AND NOT is_owner)`, userID, hash)
		if err != nil || tag.RowsAffected() != 1 {
			return true, errors.New("reviewer not found")
		}
		if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
			return true, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome) SELECT gen_random_uuid(),id,'reviewer.password_reset','user',$1,'success' FROM users WHERE is_owner`, userID); err != nil {
			return true, err
		}
		if err = tx.Commit(ctx); err != nil {
			return true, err
		}
		return true, encoder.Encode(map[string]bool{"password_reset": true, "sessions_revoked": true})
	}
	return true, nil
}
