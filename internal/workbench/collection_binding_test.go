package workbench

import (
	"errors"
	"testing"

	"buyersworkbench/internal/domain"
)

func TestCollectionBindingRequiresApprovedConnection(t *testing.T) {
	args := CollectionArgs{RunID: "run", ConnectionID: "connection", WorkspaceID: "workspace", Generation: 7}
	for _, pair := range [][2]string{{"lenovo_orders", "live_private"}, {"fictional_supplier", "live_private"}, {"unregistered", "fictional"}, {"fictional_supplier", "sanitized_snapshot"}} {
		if _, err := collectionBinding(args, pair[0], pair[1]); !errors.Is(err, ErrInvalid) {
			t.Fatal("unapproved connection binding accepted", pair, err)
		}
	}
	binding, err := collectionBinding(args, "fictional_supplier", "fictional")
	if err != nil || binding.Connector != (domain.Connector{Key: "fictional_supplier", Version: domain.SeedVersion}) || binding.WorkspaceID != args.WorkspaceID || binding.RunID != args.RunID || binding.ConnectionID != args.ConnectionID || binding.Generation != args.Generation {
		t.Fatal("trusted job identity was lost", err)
	}
}
