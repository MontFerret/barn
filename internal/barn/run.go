package barn

import "context"

// Validate loads the registry and validates every pinned remote release.
func Validate(ctx context.Context, root string, inspector Inspector) (*Registry, error) {
	registry, err := Load(root)
	if err != nil {
		return nil, err
	}

	if err := inspector.Resolve(ctx, registry); err != nil {
		return nil, err
	}

	return registry, nil
}
