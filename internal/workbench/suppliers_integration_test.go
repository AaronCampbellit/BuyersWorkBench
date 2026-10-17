package workbench

import (
	"errors"
	"testing"
)

func TestSupplierProfilesPersistAcrossCollectionAndReset(t *testing.T) {
	f := testFoundation(t)
	if _, err := f.admin.Exec(f.ctx, "UPDATE workspaces SET kind='presentation' WHERE id=$1", f.w); err != nil {
		t.Fatal(err)
	}
	manual, err := f.service.CreateSupplier(f.ctx, f.actor, f.w, 1, SupplierInput{Name: "  Manually entered supplier  ", Notes: "No source collection yet."})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Name != "Manually entered supplier" || manual.Alias == manual.Name || manual.ID == "" || manual.Revision != 1 || manual.Origin != "fictional" || manual.Status != "active" || manual.OrderCount != 0 || manual.ActiveConditions != 0 {
		t.Fatalf("manual profile invented metrics or lost metadata: %+v", manual)
	}
	unseeded, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(unseeded.Suppliers) != 1 || unseeded.Seeded || len(unseeded.Orders) != 0 {
		t.Fatal("manual directory did not exist independently of source seed", err)
	}
	duplicate, err := f.service.CreateSupplier(f.ctx, f.actor, f.w, 1, SupplierInput{Name: manual.Name})
	if err != nil || duplicate.ID == manual.ID || duplicate.Alias == manual.Alias {
		t.Fatal("same names must retain independent supplier identities", err)
	}
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	loaded, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(loaded.Suppliers) != 5 {
		t.Fatal("source profiles or manual profiles missing", err)
	}
	var seeded Supplier
	for _, p := range loaded.Suppliers {
		if p.OrderCount > 0 {
			seeded = p
			break
		}
	}
	if seeded.ID == "" {
		t.Fatal("seed profile has no source binding")
	}
	input := seeded.SupplierInput
	input.Name = "Edited supplier name"
	input.ContactName = "Fictional contact"
	input.Email = "contact@example.test"
	input.Status = "inactive"
	edited, err := f.service.UpdateSupplier(f.ctx, f.actor, f.w, seeded.ID, 1, seeded.Revision, input)
	if err != nil || edited.Alias != seeded.Alias || edited.Revision != seeded.Revision+1 || edited.OrderCount != seeded.OrderCount {
		t.Fatal("display edit changed matching identity or metrics", err)
	}
	if _, err = f.service.UpdateSupplier(f.ctx, f.actor, f.w, seeded.ID, 1, seeded.Revision, input); !errors.Is(err, ErrStale) {
		t.Fatal("stale profile edit overwrote newer fields")
	}
	refresh, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, refresh)
	loaded, err = f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range loaded.Suppliers {
		if p.ID == edited.ID {
			found = true
			if p.Name != edited.Name || p.ContactName != edited.ContactName || p.Email != edited.Email || p.Status != "inactive" || p.Revision != edited.Revision || p.Alias != seeded.Alias {
				t.Fatal("collection overwrote manual supplier edits")
			}
		}
	}
	if !found {
		t.Fatal("edited source supplier disappeared")
	}
	reset, err := f.service.Reset(f.ctx, f.actor, f.w, 1, "supplier-reset-test")
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, reset.Run)
	resetView, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(resetView.Suppliers) != 3 {
		t.Fatal("reset did not restore baseline directory", err)
	}
	for _, p := range resetView.Suppliers {
		if p.ID == manual.ID || p.ID == duplicate.ID || p.ID == edited.ID || p.Name != p.Alias || p.ContactName != "" || p.Revision != 1 || p.Status != "active" {
			t.Fatal("reset reused edited or added profiles instead of pinned defaults")
		}
	}
	if _, err = f.service.UpdateSupplier(f.ctx, f.actor, f.w, edited.ID, 1, edited.Revision, input); !errors.Is(err, ErrStale) {
		t.Fatal("old-generation supplier form remained writable")
	}
}

func TestSupplierPrivateDirectoryAndRuntimeMutationBoundary(t *testing.T) {
	f := testFoundation(t)
	var private string
	if err := f.admin.QueryRow(f.ctx, "INSERT INTO workspaces(id,owner_user_id,kind,name) VALUES(gen_random_uuid(),$1,'private','Private suppliers') RETURNING id::text", f.actor).Scan(&private); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, "INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'owner')", private, f.actor); err != nil {
		t.Fatal(err)
	}
	profile, err := f.service.CreateSupplier(f.ctx, f.actor, private, 1, SupplierInput{Name: "Private owner supplier", ContactName: "Owner controlled", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.service.Overview(f.ctx, f.actor, private)
	if err != nil || len(view.Suppliers) != 1 || view.Suppliers[0].Origin != "manual_private" || view.Seeded || len(view.Connections) != 0 || len(view.Orders) != 0 || view.Origin != "no_supplier_data" {
		t.Fatal("private manual directory fabricated a supplier connection or orders", err)
	}
	if _, err = f.service.Overview(f.ctx, f.otherActor, private); !errors.Is(err, ErrNotFound) {
		t.Fatal("reviewer read private supplier directory")
	}
	if _, err = f.service.CreateSupplier(f.ctx, f.otherActor, f.otherW, 1, SupplierInput{Name: "Reviewer mutation"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("reviewer created a directory profile")
	}
	if _, err = f.service.CreateSupplier(f.ctx, f.actor, f.w, 1, SupplierInput{Name: "Owner in reviewer sandbox"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner mutated a reviewer sandbox directory")
	}
	tx, err := f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.otherActor); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(f.ctx, "SELECT count(*) FROM wb_supplier_profiles WHERE workspace_id=$1", private).Scan(&count); err != nil || count != 0 {
		t.Fatal("runtime RLS exposed private profile", err)
	}
	if _, err = tx.Exec(f.ctx, `INSERT INTO wb_supplier_profiles(workspace_id,generation,alias,name,origin) VALUES($1,1,'reviewer-edit','Should fail','fictional')`, f.otherW); err == nil {
		t.Fatal("runtime reviewer bypassed owner-only insert policy")
	}
	tx.Rollback(f.ctx)
	seed, err := f.service.Seed(f.ctx, f.otherActor, f.otherW, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Reviewer collection must still create name-only source profiles without
	// granting manual profile edits or access to private contacts.
	var fence int64
	if err = f.admin.QueryRow(f.ctx, "SELECT fence FROM wb_runs WHERE id=$1", seed.ID).Scan(&fence); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Collect(f.ctx, CollectionArgs{1, seed.ID, seed.ConnectionID, f.otherW, 1, f.otherActor, fence}); err != nil {
		t.Fatal(err)
	}
	reviewerView, err := f.service.Overview(f.ctx, f.otherActor, f.otherW)
	if err != nil || len(reviewerView.Suppliers) != 3 {
		t.Fatal("reviewer source pipeline could not ensure profiles", err)
	}
	for _, p := range reviewerView.Suppliers {
		if p.ID == profile.ID || p.ContactName != "" || p.Origin != "fictional" {
			t.Fatal("private manual contact data crossed into reviewer seed")
		}
	}
	tx, err = f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.otherActor); err != nil {
		t.Fatal(err)
	}
	changed, err := tx.Exec(f.ctx, "UPDATE wb_supplier_profiles SET name='unauthorized edit' WHERE workspace_id=$1", f.otherW)
	if err != nil || changed.RowsAffected() != 0 {
		t.Fatal("runtime reviewer updated read-only directory", err)
	}
}
