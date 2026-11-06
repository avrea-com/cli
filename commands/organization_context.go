package commands

import (
	"context"
	"sync"

	"github.com/avrea-com/cli/config"
)

type organizationSlugsKey struct{}
type organizationScope struct{ host, token, id string }
type organizationSlugs struct {
	mu     sync.Mutex
	values map[organizationScope]string
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
