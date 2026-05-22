package config

import (
	"sort"
	"strings"

	"github.com/charmbracelet/crush/internal/env"
)

// withCrushEnvAliases returns an Env that exposes CRUSH_FOO as FOO for
// configuration-time expansion without mutating the host process environment.
//
// Historically configureProviders temporarily copied every CRUSH_* variable
// into its non-prefixed counterpart with os.Setenv so catwalk defaults such as
// $ANTHROPIC_API_KEY could be satisfied by CRUSH_ANTHROPIC_API_KEY. That is a
// process-wide side effect and is unsafe when Crush is embedded in a process
// that may start multiple Crush apps concurrently. Keep the compatibility alias
// at the resolver boundary instead.
func withCrushEnvAliases(base env.Env) env.Env {
	if base == nil {
		base = env.NewFromMap(nil)
	}
	return crushAliasEnv{base: base}
}

type crushAliasEnv struct {
	base env.Env
}

func (e crushAliasEnv) Get(key string) string {
	if value, ok := e.aliases()[key]; ok {
		return value
	}
	return e.base.Get(key)
}

func (e crushAliasEnv) Env() []string {
	merged := envMap(e.base.Env())
	for key, value := range e.aliases() {
		merged[key] = value
	}

	out := make([]string, 0, len(merged))
	for key, value := range merged {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

func (e crushAliasEnv) aliases() map[string]string {
	aliases := make(map[string]string)
	for _, item := range e.base.Env() {
		key, value, ok := strings.Cut(item, "=")
		if !ok || !strings.HasPrefix(key, "CRUSH_") {
			continue
		}
		alias := strings.TrimPrefix(key, "CRUSH_")
		if alias == "" {
			continue
		}
		aliases[alias] = value
	}
	return aliases
}

func envMap(items []string) map[string]string {
	out := make(map[string]string, len(items))
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}
