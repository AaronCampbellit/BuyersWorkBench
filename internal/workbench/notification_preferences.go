package workbench

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type NotificationPreferencesInput struct {
	StatusChanges      bool `json:"status_changes"`
	SupplierConditions bool `json:"supplier_conditions"`
}

type NotificationPreferences struct {
	NotificationPreferencesInput
	Revision int64 `json:"revision"`
}

type WorkspaceMember struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

func notificationPreferencesTx(ctx context.Context, tx pgx.Tx, actor, workspace string) (NotificationPreferences, error) {
	p := NotificationPreferences{NotificationPreferencesInput: NotificationPreferencesInput{true, true}, Revision: 0}
	err := tx.QueryRow(ctx, `SELECT status_changes,supplier_conditions,revision FROM wb_notification_preferences
 WHERE workspace_id=$1 AND user_id=$2`, workspace, actor).Scan(&p.StatusChanges, &p.SupplierConditions, &p.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return p, err
}

func (s *Service) GetNotificationPreferences(ctx context.Context, actor, workspace string) (NotificationPreferences, error) {
	tx, _, err := s.begin(ctx, actor, workspace, 0, false)
	if err != nil {
		return NotificationPreferences{}, err
	}
	defer tx.Rollback(ctx)
	p, err := notificationPreferencesTx(ctx, tx, actor, workspace)
	if err != nil {
		return NotificationPreferences{}, err
	}
	return p, tx.Commit(ctx)
}

// SetNotificationPreferences serializes on the workspace and uses revision zero
// for the default, unpersisted preferences. Repeating the last accepted update
// with the same values is safe after a lost HTTP response.
func (s *Service) SetNotificationPreferences(ctx context.Context, actor, workspace string, generation, revision int64, input NotificationPreferencesInput) (NotificationPreferences, error) {
	if generation < 1 || revision < 0 {
		return NotificationPreferences{}, ErrInvalid
	}
	tx, _, err := s.begin(ctx, actor, workspace, generation, true)
	if err != nil {
		return NotificationPreferences{}, err
	}
	defer tx.Rollback(ctx)
	current, err := notificationPreferencesTx(ctx, tx, actor, workspace)
	if err != nil {
		return NotificationPreferences{}, err
	}
	if current.Revision != revision {
		if current.Revision == revision+1 && current.NotificationPreferencesInput == input {
			return current, tx.Commit(ctx)
		}
		return NotificationPreferences{}, ErrStale
	}
	if current.NotificationPreferencesInput == input {
		return current, tx.Commit(ctx)
	}
	result := NotificationPreferences{NotificationPreferencesInput: input, Revision: revision + 1}
	_, err = tx.Exec(ctx, `INSERT INTO wb_notification_preferences(workspace_id,user_id,status_changes,supplier_conditions,revision)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(workspace_id,user_id) DO UPDATE SET
 status_changes=excluded.status_changes,supplier_conditions=excluded.supplier_conditions,revision=excluded.revision,updated_at=now()`,
		workspace, actor, input.StatusChanges, input.SupplierConditions, result.Revision)
	if err != nil {
		return NotificationPreferences{}, err
	}
	if err = audit(ctx, tx, actor, workspace, "notifications.preferences.update"); err != nil {
		return NotificationPreferences{}, err
	}
	return result, tx.Commit(ctx)
}

// WorkspaceMembers exposes only active member identities within the caller's
// authorized workspace. It never exposes account emails or foreign members.
func (s *Service) WorkspaceMembers(ctx context.Context, actor, workspace string) ([]WorkspaceMember, error) {
	tx, _, err := s.begin(ctx, actor, workspace, 0, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT u.id::text,u.display_name FROM memberships m JOIN users u ON u.id=m.user_id
 WHERE m.workspace_id=$1 AND u.active AND u.disabled_at IS NULL AND (u.expires_at IS NULL OR u.expires_at>now())
 ORDER BY lower(u.display_name),u.id LIMIT 500`, workspace)
	if err != nil {
		return nil, err
	}
	members := []WorkspaceMember{}
	for rows.Next() {
		var member WorkspaceMember
		if err = rows.Scan(&member.ID, &member.DisplayName); err != nil {
			rows.Close()
			return nil, err
		}
		members = append(members, member)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return members, tx.Commit(ctx)
}

func notifyConditionTx(ctx context.Context, tx pgx.Tx, args CollectionArgs, conditionID, eventKey, message string) error {
	// Pin the single condition and its order before routing. A direct join can
	// make the RLS planner rescan the entire order scope for every condition.
	_, err := tx.Exec(ctx, `WITH subject AS MATERIALIZED (
 SELECT id,order_id,assignee_id FROM wb_conditions WHERE workspace_id=$1 AND generation=$2 AND id=$3
), observed_source AS MATERIALIZED (
 SELECT id FROM wb_orders WHERE id=(SELECT order_id FROM subject)
 AND workspace_id=$1 AND generation=$2 AND connection_id=$6
), routing AS MATERIALIZED (
 SELECT buyer_user_id FROM wb_connections WHERE workspace_id=$1 AND id=$6
)
INSERT INTO wb_notifications(workspace_id,generation,condition_id,recipient_id,event_key,message)
 SELECT $1,$2,subject.id,recipient.user_id,$4,$5
 FROM subject CROSS JOIN observed_source CROSS JOIN routing
 CROSS JOIN LATERAL bwb_notification_recipients($1,subject.assignee_id,routing.buyer_user_id,'supplier_conditions') recipient
 ON CONFLICT DO NOTHING`, args.WorkspaceID, args.Generation, conditionID, eventKey, message, args.ConnectionID)
	return err
}
