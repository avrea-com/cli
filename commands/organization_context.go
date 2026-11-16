package commands

import (
	"context"
	"maps"
	"sync"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

type organizationSlugsKey struct{}
type organizationScope struct{ host, token, id string }
type organizationSlugs struct {
	mu         sync.Mutex
	values     map[organizationScope]string
	identities map[organizationScope]map[string]any
}

// Reuse successful organization resolution for display links within one
// ExecuteRoot call. Never persist credentials/results or cache lookup failures.
// A new invocation always gets new state, even when its Cobra tree is reused.
func rememberOrganizationSlug(ctx context.Context, cfg config.Config, id, slug string) {
	state, _ := ctx.Value(organizationSlugsKey{}).(*organizationSlugs)
	if state == nil || slug == "" {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.values == nil {
		state.values = make(map[organizationScope]string)
	}
	state.values[organizationScope{cfg.Host, cfg.Token, id}] = slug
}

func knownOrganizationSlug(ctx context.Context, cfg config.Config, id string) string {
	state, _ := ctx.Value(organizationSlugsKey{}).(*organizationSlugs)
	if state == nil {
		return ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.values[organizationScope{cfg.Host, cfg.Token, id}]
}

// serviceIdentity reads /service-tokens/self at most once per ExecuteRoot call
// and credential: service tokens are rate limited, and organization resolution
// and status output in one command need the same answer. Failures are not
// cached. Callers receive a copy they may change.
func serviceIdentity(ctx context.Context, cfg config.Config, api *client.Client) (map[string]any, error) {
	state, _ := ctx.Value(organizationSlugsKey{}).(*organizationSlugs)
	scope := organizationScope{host: cfg.Host, token: cfg.Token}
	if state != nil {
		state.mu.Lock()
		identity, ok := state.identities[scope]
		state.mu.Unlock()
		if ok {
			return maps.Clone(identity), nil
		}
	}
	identity, err := api.ServiceTokenIdentity(ctx)
	if err != nil || state == nil {
		return identity, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.identities == nil {
		state.identities = make(map[organizationScope]map[string]any)
	}
	state.identities[scope] = identity
	return maps.Clone(identity), nil
}
