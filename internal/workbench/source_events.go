package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
)

// publishSourceChange appends the canonical source event and any notice in the
// accepted-batch transaction. The run/order/field identity deduplicates retries;
// subsequent A-to-B-to-A transitions retain their own events and notices.
func publishSourceChange(ctx context.Context, tx pgx.Tx, args CollectionArgs, orderID string, change domain.Change) error {
	var eventID string
	err := tx.QueryRow(ctx, `INSERT INTO wb_changes(workspace_id,generation,order_id,run_id,field,data)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(run_id,order_id,field) DO NOTHING RETURNING id::text`,
		args.WorkspaceID, args.Generation, orderID, args.RunID, change.Field, raw(change)).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || change.Field != "status" {
		return err
	}
	message := fmt.Sprintf("Order status changed from %s to %s.", strings.ReplaceAll(change.Before, "_", " "), strings.ReplaceAll(change.After, "_", " "))
	_, err = tx.Exec(ctx, `INSERT INTO wb_notifications(workspace_id,generation,source_event_id,recipient_id,event_key,message)
 SELECT $1,$2,$3,recipient.user_id,$4,$5 FROM wb_connections connection
 CROSS JOIN LATERAL bwb_notification_recipients($1,NULL,connection.buyer_user_id,'status_changes') recipient
 WHERE connection.id=$6 AND connection.workspace_id=$1
 ON CONFLICT DO NOTHING`, args.WorkspaceID, args.Generation, eventID, "source_event:"+eventID, message, args.ConnectionID)
	return err
}
