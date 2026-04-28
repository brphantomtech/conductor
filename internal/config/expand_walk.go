package config

import "fmt"

// expandReferences applies $VAR substitution to the string fields SPEC §6.2
// classifies as path or credential. URI fields and shell-command fields are
// passed through verbatim by design. The walk is deliberately explicit
// (rather than reflection-driven) so it is obvious from the source which
// fields participate.
func expandReferences(cfg *Config, lookup LookupFunc) error {
	if cfg == nil {
		return nil
	}
	if lookup == nil {
		lookup = OSLookup
	}

	// Tracker credentials.
	if err := expandField(&cfg.Tracker.APIKey, "tracker.api_key", lookup); err != nil {
		return err
	}

	// Workspace root: path field, both $VAR and ~ apply.
	if err := normalizePathField(&cfg.Workspace.Root, "workspace.root", lookup); err != nil {
		return err
	}
	for i := range cfg.Workspace.Repos {
		if err := expandField(&cfg.Workspace.Repos[i].URL,
			fmt.Sprintf("workspace.repos[%d].url", i), lookup); err != nil {
			return err
		}
	}

	// Provider keys (default + per-role).
	if err := expandField(&cfg.Providers.Default.APIKey, "providers.default.api_key", lookup); err != nil {
		return err
	}
	for role, p := range cfg.Providers.Roles {
		if err := expandField(&p.APIKey, fmt.Sprintf("providers.%s.api_key", role), lookup); err != nil {
			return err
		}
		cfg.Providers.Roles[role] = p
	}

	// Knowledge / memory store paths.
	if err := normalizePathField(&cfg.Knowledge.StorePath, "knowledge.store_path", lookup); err != nil {
		return err
	}
	if err := normalizePathField(&cfg.Memory.StorePath, "memory.store_path", lookup); err != nil {
		return err
	}

	// Doc store path/url and auth values.
	for i := range cfg.Docs.Stores {
		store := &cfg.Docs.Stores[i]
		if err := expandField(&store.PathOrURL,
			fmt.Sprintf("docs.stores[%d].path_or_url", i), lookup); err != nil {
			return err
		}
		for k, v := range store.Auth {
			expanded, err := Expand(v, lookup)
			if err != nil {
				return fmt.Errorf("config: docs.stores[%d].auth.%s: %w", i, k, err)
			}
			store.Auth[k] = expanded
		}
	}

	return nil
}

func expandField(target *string, label string, lookup LookupFunc) error {
	if target == nil || *target == "" {
		return nil
	}
	expanded, err := Expand(*target, lookup)
	if err != nil {
		return fmt.Errorf("config: %s: %w", label, err)
	}
	*target = expanded
	return nil
}

func normalizePathField(target *string, label string, lookup LookupFunc) error {
	if target == nil || *target == "" {
		return nil
	}
	expanded, err := NormalizePath(*target, lookup)
	if err != nil {
		return fmt.Errorf("config: %s: %w", label, err)
	}
	*target = expanded
	return nil
}
