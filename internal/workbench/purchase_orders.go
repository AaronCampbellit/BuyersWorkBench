package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
)

const purchaseOrderQuery = `SELECT p.id::text,p.number,p.generation,p.data,p.supplier_alias,p.supplier_name,
 p.origin,p.status,p.received_quantity::text,p.placement_reference,COALESCE(p.source_quote_id,''),
 p.quote_source,p.revision,p.created_at,p.updated_at FROM wb_purchase_orders p WHERE p.workspace_id=$1 AND p.generation=$2`

func purchaseOrderRow(row interface{ Scan(...any) error }) (PurchaseOrder, error) {
	var order PurchaseOrder
	var data, quoteSource []byte
	err := row.Scan(&order.ID, &order.Number, &order.Generation, &data, &order.SupplierAlias, &order.SupplierName,
		&order.Origin, &order.Status, &order.ReceivedQuantity, &order.PlacementReference, &order.SourceQuoteID,
		&quoteSource, &order.Revision, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return order, ErrNotFound
	}
	if err != nil {
		return order, err
	}
	if err = json.Unmarshal(data, &order.PurchaseOrderInput); err != nil {
		return order, err
	}
	if len(quoteSource) > 0 {
		var source PurchaseOrderQuoteSource
		if err = json.Unmarshal(quoteSource, &source); err != nil {
			return order, err
		}
		order.QuoteSource = &source
	}
	order.ReceivedQuantity = compactPOQuantity(order.ReceivedQuantity)
	order.ModeledTotal, err = domain.PurchaseOrderTotal(order.PurchaseOrderInput)
	order.AllowedActions = domain.PurchaseOrderActions(order.Status)
	if order.AllowedActions == nil {
		order.AllowedActions = []string{}
	}
	return order, err
}

func compactPOQuantity(quantity string) string {
	if strings.Contains(quantity, ".") {
		quantity = strings.TrimRight(strings.TrimRight(quantity, "0"), ".")
	}
	return quantity
}

// The overview and dedicated list share the same transaction and current scope.
func purchaseOrdersTx(ctx context.Context, tx pgx.Tx, w string, g int64) ([]PurchaseOrder, error) {
	orders := []PurchaseOrder{}
	rows, err := tx.Query(ctx, purchaseOrderQuery+" ORDER BY p.created_at DESC,p.id DESC LIMIT 500", w, g)
	if err != nil {
		return orders, err
	}
	defer rows.Close()
	for rows.Next() {
		order, err := purchaseOrderRow(rows)
		if err != nil {
			return orders, err
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func (s *Service) PurchaseOrder(ctx context.Context, actor, w, id string) (PurchaseOrderDetail, error) {
	result := PurchaseOrderDetail{Events: []PurchaseOrderEvent{}}
	if !uuidRE.MatchString(id) {
		return result, ErrNotFound
	}
	tx, sc, err := s.begin(ctx, actor, w, 0, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	result.Order, err = purchaseOrderRow(tx.QueryRow(ctx, purchaseOrderQuery+" AND p.id=$3", w, sc.generation, id))
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT e.id::text,e.action,e.from_status,e.to_status,e.quantity,e.reference,e.note,u.display_name,e.created_at
		FROM wb_purchase_order_events e JOIN users u ON u.id=e.actor_id
		WHERE e.workspace_id=$1 AND e.generation=$2 AND e.order_id=$3 ORDER BY e.created_at DESC,e.id DESC LIMIT 200`, w, sc.generation, id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var event PurchaseOrderEvent
		if err = rows.Scan(&event.ID, &event.Action, &event.FromStatus, &event.ToStatus, &event.Quantity, &event.Reference, &event.Note, &event.ActorName, &event.CreatedAt); err != nil {
			return result, err
		}
		result.Events = append(result.Events, event)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func validPOKey(key string) bool {
	return len(key) >= 8 && len(key) <= 128 && utf8.ValidString(key) && strings.TrimSpace(key) == key &&
		!strings.ContainsFunc(key, unicode.IsControl)
}

func purchaseSupplier(ctx context.Context, tx pgx.Tx, w string, g int64, id string) (string, string, error) {
	if !uuidRE.MatchString(id) {
		return "", "", ErrInvalid
	}
	var alias, name, status string
	err := tx.QueryRow(ctx, "SELECT alias,name,status FROM wb_supplier_profiles WHERE workspace_id=$1 AND generation=$2 AND id=$3", w, g, id).Scan(&alias, &name, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if status != "active" {
		return "", "", ErrInvalid
	}
	return alias, name, nil
}

func purchaseOrderEvent(ctx context.Context, tx pgx.Tx, actor, w, id string, g int64, action, from, to, quantity, reference, note string) error {
	_, err := tx.Exec(ctx, `INSERT INTO wb_purchase_order_events(workspace_id,generation,order_id,actor_id,action,from_status,to_status,quantity,reference,note)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, w, g, id, actor, action, from, to, quantity, reference, note)
	return err
}

func insertPurchaseOrder(ctx context.Context, tx pgx.Tx, actor, w string, sc scope, input domain.PurchaseOrderInput, sourceID string, source *PurchaseOrderQuoteSource) (PurchaseOrder, error) {
	alias, name, err := purchaseSupplier(ctx, tx, w, sc.generation, input.SupplierID)
	if err != nil {
		return PurchaseOrder{}, err
	}
	origin := "fictional"
	if sc.kind == "private" {
		origin = "manual_private"
	}
	var quoteID, quoteData any
	if source != nil {
		quoteID, quoteData = sourceID, raw(source)
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO wb_purchase_orders(workspace_id,generation,supplier_id,data,supplier_alias,supplier_name,origin,created_by,source_quote_id,quote_source)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id::text`, w, sc.generation, input.SupplierID, raw(input), alias, name, origin, actor, quoteID, quoteData).Scan(&id)
	if err != nil {
		return PurchaseOrder{}, err
	}
	return purchaseOrderRow(tx.QueryRow(ctx, purchaseOrderQuery+" AND p.id=$3", w, sc.generation, id))
}

func (s *Service) CreatePurchaseOrder(ctx context.Context, actor, w string, g int64, key string, input domain.PurchaseOrderInput) (PurchaseOrder, error) {
	if g < 1 || !validPOKey(key) {
		return PurchaseOrder{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return PurchaseOrder{}, err
	}
	defer tx.Rollback(ctx)
	if err = supplierOwner(ctx, tx, sc, actor, w); err != nil {
		return PurchaseOrder{}, err
	}
	input, err = domain.NormalizePurchaseOrder(input)
	if err != nil {
		return PurchaseOrder{}, ErrInvalid
	}
	payload := string(raw(input))
	var result PurchaseOrder
	if found, err := operation(ctx, tx, actor, w, "purchase_order.create", key, payload, g, &result); err != nil {
		return result, err
	} else if found {
		return result, tx.Commit(ctx)
	}
	result, err = insertPurchaseOrder(ctx, tx, actor, w, sc, input, "", nil)
	if err != nil {
		return result, err
	}
	if err = purchaseOrderEvent(ctx, tx, actor, w, result.ID, g, "create", "", "draft", input.Quantity, "", ""); err != nil {
		return result, err
	}
	if err = saveOperation(ctx, tx, actor, w, "purchase_order.create", key, payload, g, result); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, actor, w, "purchase_order.create"); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Service) CreatePurchaseOrderFromQuote(ctx context.Context, actor, w, quoteID string, g, revision int64, key string) (PurchaseOrder, error) {
	if g < 1 || revision < 1 || !validPOKey(key) || quoteID == "" || len(quoteID) > 240 {
		return PurchaseOrder{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return PurchaseOrder{}, err
	}
	defer tx.Rollback(ctx)
	if err = supplierOwner(ctx, tx, sc, actor, w); err != nil {
		return PurchaseOrder{}, err
	}
	payload := string(raw(struct {
		QuoteID  string
		Revision int64
	}{quoteID, revision}))
	var result PurchaseOrder
	const kind = "purchase_order.from_quote"
	if found, err := operation(ctx, tx, actor, w, kind, key, payload, g, &result); err != nil {
		return result, err
	} else if found {
		return result, tx.Commit(ctx)
	}
	// The generation permits one order from each offer. A separate click with a
	// fresh key returns the existing record instead of duplicating purchasing.
	result, err = purchaseOrderRow(tx.QueryRow(ctx, purchaseOrderQuery+" AND p.source_quote_id=$3", w, g, quoteID))
	if err == nil {
		if err = saveOperation(ctx, tx, actor, w, kind, key, payload, g, result); err != nil {
			return result, err
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, ErrNotFound) {
		return result, err
	}
	quotes, err := quotesTx(ctx, tx, w, g)
	if err != nil {
		return result, err
	}
	if quotes.Revision != revision {
		return result, ErrStale
	}
	aliases := map[string]string{"atlas": "fictional_atlas", "meridian": "fictional_meridian", "harbor": "fictional_harbor"}
	alias, known := aliases[quoteID]
	if !known || quotes.Comparison.ID != domain.SeedQuotes().ID || quotes.Comparison.Origin != "fictional" {
		return result, ErrNotFound
	}
	var selected *domain.Quote
	for _, offer := range quotes.Calculation.Results {
		if offer.ID == quoteID {
			if !offer.Eligible {
				return result, ErrInvalid
			}
			quote := offer.Quote
			selected = &quote
			break
		}
	}
	if selected == nil {
		return result, ErrNotFound
	}
	var supplierID string
	err = tx.QueryRow(ctx, "SELECT id::text FROM wb_supplier_profiles WHERE workspace_id=$1 AND generation=$2 AND alias=$3", w, g, alias).Scan(&supplierID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	input := domain.PurchaseOrderInput{SupplierID: supplierID, Description: quotes.Comparison.Description, Quantity: quotes.Comparison.Quantity,
		UOM: quotes.Comparison.UOM, Currency: quotes.Comparison.Currency, UnitPrice: selected.UnitPrice, Freight: selected.Freight,
		OtherCharges: selected.OtherCharges, RequiredDate: quotes.Comparison.RequiredDate}
	input, err = domain.NormalizePurchaseOrder(input)
	if err != nil {
		return result, ErrInvalid
	}
	source := &PurchaseOrderQuoteSource{ComparisonID: quotes.Comparison.ID, Revision: quotes.Revision, Quote: *selected,
		Quantity: quotes.Comparison.Quantity, Description: quotes.Comparison.Description, RequiredDate: quotes.Comparison.RequiredDate,
		ExcludedCosts: append([]string{}, quotes.Comparison.ExcludedCosts...)}
	result, err = insertPurchaseOrder(ctx, tx, actor, w, sc, input, quoteID, source)
	if err != nil {
		return result, err
	}
	if err = purchaseOrderEvent(ctx, tx, actor, w, result.ID, g, "create_from_quote", "", "draft", input.Quantity, "", ""); err != nil {
		return result, err
	}
	if err = saveOperation(ctx, tx, actor, w, kind, key, payload, g, result); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, actor, w, kind); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Service) UpdatePurchaseOrder(ctx context.Context, actor, w, id string, g, revision int64, input domain.PurchaseOrderInput) (PurchaseOrder, error) {
	if g < 1 || revision < 1 || !uuidRE.MatchString(id) {
		return PurchaseOrder{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return PurchaseOrder{}, err
	}
	defer tx.Rollback(ctx)
	if err = supplierOwner(ctx, tx, sc, actor, w); err != nil {
		return PurchaseOrder{}, err
	}
	current, err := purchaseOrderRow(tx.QueryRow(ctx, purchaseOrderQuery+" AND p.id=$3", w, g, id))
	if err != nil {
		return current, err
	}
	if current.Revision != revision {
		return current, ErrStale
	}
	if current.Status != "draft" {
		return current, ErrInvalid
	}
	input, err = domain.NormalizePurchaseOrder(input)
	if err != nil {
		return current, ErrInvalid
	}
	alias, name, err := purchaseSupplier(ctx, tx, w, g, input.SupplierID)
	if err != nil {
		return current, err
	}
	changed, err := tx.Exec(ctx, `UPDATE wb_purchase_orders SET supplier_id=$5,data=$6,supplier_alias=$7,supplier_name=$8,revision=revision+1,updated_at=now()
		WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND revision=$4`, w, g, id, revision, input.SupplierID, raw(input), alias, name)
	if err != nil {
		return current, err
	}
	if changed.RowsAffected() != 1 {
		return current, ErrStale
	}
	result, err := purchaseOrderRow(tx.QueryRow(ctx, purchaseOrderQuery+" AND p.id=$3", w, g, id))
	if err != nil {
		return result, err
	}
	if err = purchaseOrderEvent(ctx, tx, actor, w, id, g, "update_draft", current.Status, result.Status, input.Quantity, "", ""); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, actor, w, "purchase_order.update_draft"); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Service) ActOnPurchaseOrder(ctx context.Context, actor, w, id string, g, revision int64, key string, input domain.PurchaseOrderActionInput) (PurchaseOrder, error) {
	if g < 1 || revision < 1 || !uuidRE.MatchString(id) || !validPOKey(key) {
		return PurchaseOrder{}, ErrInvalid
	}
	input.Action = strings.TrimSpace(input.Action)
	input.Quantity = strings.TrimSpace(input.Quantity)
	input.Reference = strings.TrimSpace(input.Reference)
	input.Note = strings.TrimSpace(input.Note)
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return PurchaseOrder{}, err
	}
	defer tx.Rollback(ctx)
	if err = supplierOwner(ctx, tx, sc, actor, w); err != nil {
		return PurchaseOrder{}, err
	}
	payload := string(raw(struct {
		Revision int64
		Input    domain.PurchaseOrderActionInput
	}{revision, input}))
	kind := "purchase_order.action:" + id
	var result PurchaseOrder
	if found, err := operation(ctx, tx, actor, w, kind, key, payload, g, &result); err != nil {
		return result, err
	} else if found {
		return result, tx.Commit(ctx)
	}
	current, err := purchaseOrderRow(tx.QueryRow(ctx, purchaseOrderQuery+" AND p.id=$3", w, g, id))
	if err != nil {
		return current, err
	}
	if current.Revision != revision {
		return current, ErrStale
	}
	status, received, err := domain.ApplyPurchaseOrderAction(current.Status, current.Quantity, current.ReceivedQuantity, input)
	if err != nil {
		return current, ErrInvalid
	}
	if input.Action == "approve" || input.Action == "place" {
		if _, _, err = purchaseSupplier(ctx, tx, w, g, current.SupplierID); err != nil {
			return current, err
		}
	}
	placement := current.PlacementReference
	if input.Action == "place" {
		placement = input.Reference
	}
	changed, err := tx.Exec(ctx, `UPDATE wb_purchase_orders SET status=$5,received_quantity=$6::numeric,placement_reference=$7,revision=revision+1,updated_at=now()
		WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND revision=$4`, w, g, id, revision, status, received, placement)
	if err != nil {
		return current, err
	}
	if changed.RowsAffected() != 1 {
		return current, ErrStale
	}
	result, err = purchaseOrderRow(tx.QueryRow(ctx, purchaseOrderQuery+" AND p.id=$3", w, g, id))
	if err != nil {
		return result, err
	}
	if err = purchaseOrderEvent(ctx, tx, actor, w, id, g, input.Action, current.Status, result.Status, input.Quantity, input.Reference, input.Note); err != nil {
		return result, err
	}
	if err = saveOperation(ctx, tx, actor, w, kind, key, payload, g, result); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, actor, w, "purchase_order."+input.Action); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
