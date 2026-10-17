package workbench

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func supplierText(value string, maximum int, multiline bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t')) })
}
func normalizeSupplier(in SupplierInput, create bool) (SupplierInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.ContactName = strings.TrimSpace(in.ContactName)
	in.Email = strings.TrimSpace(in.Email)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Website = strings.TrimSpace(in.Website)
	in.Notes = strings.TrimSpace(in.Notes)
	in.Status = strings.TrimSpace(in.Status)
	if create && in.Status == "" {
		in.Status = "active"
	}
	if in.Name == "" || !supplierText(in.Name, 100, false) || !supplierText(in.ContactName, 100, false) || !supplierText(in.Phone, 50, false) || !supplierText(in.Notes, 4000, true) || (in.Status != "active" && in.Status != "inactive") {
		return in, ErrInvalid
	}
	if len(in.Email) > 254 || !supplierText(in.Email, 254, false) {
		return in, ErrInvalid
	}
	if in.Email != "" {
		address, err := mail.ParseAddress(in.Email)
		if err != nil || address.Address != in.Email {
			return in, ErrInvalid
		}
	}
	if len(in.Website) > 2048 || !supplierText(in.Website, 2048, false) {
		return in, ErrInvalid
	}
	if in.Website != "" {
		parsed, err := url.Parse(in.Website)
		if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(parsed.Host, " \t\r\n") {
			return in, ErrInvalid
		}
	}
	return in, nil
}

// Aggregate each scoped evidence table once. Correlated per-supplier counts
// otherwise repeatedly evaluate RLS across order/condition joins as feeds grow.
const supplierQuery = `WITH scoped_orders AS MATERIALIZED (
 SELECT id,record->>'supplier_alias' AS alias FROM wb_orders WHERE workspace_id=$1 AND generation=$2
), order_totals AS (
 SELECT alias,count(*)::integer AS count FROM scoped_orders GROUP BY alias
), condition_totals AS (
 SELECT o.alias,count(*)::integer AS count FROM wb_conditions c JOIN scoped_orders o ON o.id=c.order_id
 WHERE c.workspace_id=$1 AND c.generation=$2 AND c.condition_active GROUP BY o.alias
)
SELECT p.id::text,p.alias,p.name,p.contact_name,p.email,p.phone,p.website,p.notes,p.status,p.revision,p.origin,
 COALESCE(ot.count,0),COALESCE(ct.count,0)
 FROM wb_supplier_profiles p LEFT JOIN order_totals ot ON ot.alias=p.alias LEFT JOIN condition_totals ct ON ct.alias=p.alias
 WHERE p.workspace_id=$1 AND p.generation=$2`

func supplierRow(row interface{ Scan(...any) error }) (Supplier, error) {
	var s Supplier
	err := row.Scan(&s.ID, &s.Alias, &s.Name, &s.ContactName, &s.Email, &s.Phone, &s.Website, &s.Notes, &s.Status, &s.Revision, &s.Origin, &s.OrderCount, &s.ActiveConditions)
	s.CoverageLabel = "No collected orders; supplier performance unavailable."
	if s.OrderCount > 0 {
		s.CoverageLabel = "Current fictional orders and observed changes; on-time delivery performance unavailable."
		if s.Origin == "manual_private" {
			s.CoverageLabel = "Current private observations; on-time delivery performance unavailable."
		}
	}
	return s, err
}
func suppliersTx(ctx context.Context, tx pgx.Tx, w string, g int64) ([]Supplier, error) {
	suppliers := []Supplier{}
	rows, err := tx.Query(ctx, supplierQuery+" ORDER BY lower(p.name),p.alias,p.id LIMIT 500", w, g)
	if err != nil {
		return suppliers, err
	}
	defer rows.Close()
	for rows.Next() {
		supplier, err := supplierRow(rows)
		if err != nil {
			return suppliers, err
		}
		suppliers = append(suppliers, supplier)
	}
	return suppliers, rows.Err()
}
func supplierOwner(ctx context.Context, tx pgx.Tx, sc scope, actor, w string) error {
	if !sc.isOwner || sc.kind == "reviewer" {
		return ErrNotFound
	}
	var owns bool
	if err := tx.QueryRow(ctx, "SELECT owner_user_id=$2 FROM workspaces WHERE id=$1", w, actor).Scan(&owns); err != nil {
		return err
	}
	if !owns {
		return ErrNotFound
	}
	return nil
}

func (s *Service) CreateSupplier(ctx context.Context, actor, w string, g int64, input SupplierInput) (Supplier, error) {
	if g < 1 {
		return Supplier{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Supplier{}, err
	}
	defer tx.Rollback(ctx)
	if err = supplierOwner(ctx, tx, sc, actor, w); err != nil {
		return Supplier{}, err
	}
	input, err = normalizeSupplier(input, true)
	if err != nil {
		return Supplier{}, err
	}
	origin := "fictional"
	if sc.kind == "private" {
		origin = "manual_private"
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO wb_supplier_profiles(workspace_id,generation,alias,name,contact_name,email,phone,website,notes,status,origin)
 VALUES($1,$2,'manual-'||gen_random_uuid()::text,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id::text`, w, g, input.Name, input.ContactName, input.Email, input.Phone, input.Website, input.Notes, input.Status, origin).Scan(&id)
	if err != nil {
		return Supplier{}, err
	}
	result, err := supplierRow(tx.QueryRow(ctx, supplierQuery+" AND p.id=$3", w, g, id))
	if err != nil {
		return Supplier{}, err
	}
	if err = audit(ctx, tx, actor, w, "supplier.create"); err != nil {
		return Supplier{}, err
	}
	return result, tx.Commit(ctx)
}
func (s *Service) UpdateSupplier(ctx context.Context, actor, w, id string, g, revision int64, input SupplierInput) (Supplier, error) {
	if !uuidRE.MatchString(id) || g < 1 || revision < 1 {
		return Supplier{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Supplier{}, err
	}
	defer tx.Rollback(ctx)
	if err = supplierOwner(ctx, tx, sc, actor, w); err != nil {
		return Supplier{}, err
	}
	input, err = normalizeSupplier(input, false)
	if err != nil {
		return Supplier{}, err
	}
	var currentRevision int64
	err = tx.QueryRow(ctx, "SELECT revision FROM wb_supplier_profiles WHERE workspace_id=$1 AND generation=$2 AND id=$3", w, g, id).Scan(&currentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return Supplier{}, ErrNotFound
	}
	if err != nil {
		return Supplier{}, err
	}
	if currentRevision != revision {
		return Supplier{}, ErrStale
	}
	changed, err := tx.Exec(ctx, `UPDATE wb_supplier_profiles SET name=$5,contact_name=$6,email=$7,phone=$8,website=$9,notes=$10,status=$11,revision=revision+1,updated_at=now() WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND revision=$4`, w, g, id, revision, input.Name, input.ContactName, input.Email, input.Phone, input.Website, input.Notes, input.Status)
	if err != nil {
		return Supplier{}, err
	}
	if changed.RowsAffected() != 1 {
		return Supplier{}, ErrStale
	}
	result, err := supplierRow(tx.QueryRow(ctx, supplierQuery+" AND p.id=$3", w, g, id))
	if err != nil {
		return Supplier{}, err
	}
	if err = audit(ctx, tx, actor, w, "supplier.update"); err != nil {
		return Supplier{}, err
	}
	return result, tx.Commit(ctx)
}

func ensureSupplierTx(ctx context.Context, tx pgx.Tx, w string, g int64, alias string) error {
	_, err := tx.Exec(ctx, "SELECT bwb_ensure_supplier_profile($1,$2,$3)", w, g, alias)
	return err
}
