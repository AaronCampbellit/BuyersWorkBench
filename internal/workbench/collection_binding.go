package workbench

import (
	"buyersworkbench/internal/collection"
	"buyersworkbench/internal/domain"
)

// collectionBinding resolves durable connection configuration, never source
// claims. Only the fictional connection currently has an approved account/scope
// mapping. Registering another adapter alone cannot enable supplier access.
func collectionBinding(args CollectionArgs, key, origin string) (domain.Binding, error) {
	if key != "fictional_supplier" || origin != "fictional" {
		return domain.Binding{}, ErrInvalid
	}
	descriptor, err := collection.DefaultRegistry().Describe(key)
	if err != nil || string(descriptor.Mode) != origin {
		return domain.Binding{}, ErrInvalid
	}
	return domain.Binding{
		RunID: args.RunID, ConnectionID: args.ConnectionID,
		WorkspaceID: args.WorkspaceID, Generation: args.Generation,
		Connector: domain.Connector{Key: descriptor.Key, Version: descriptor.Version},
		Origin:    origin, Scope: domain.FictionalScope(),
	}, nil
}

// Additional fictional feeds support all normalized statuses while the pinned
// scenario feed retains its original, explicit demonstration scope.
func collectionBindingForFeed(args CollectionArgs, key, origin string, isDefault bool) (domain.Binding, error) {
	binding, err := collectionBinding(args, key, origin)
	if err == nil && !isDefault {
		binding.Scope.Statuses = []string{"open", "partially_shipped", "shipped", "delivered", "cancelled", "unknown"}
	}
	return binding, err
}
