package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A clone's origin can be stale: when a repository is renamed or transferred
// between organisations, GitHub redirects but the local remote keeps pointing
// at the old path. Comparing those strings directly makes a cloned repo look
// like it was never cloned, so remotes are resolved to their canonical
// owner/name before anything is matched against them.
//
// Results are cached on disk because they change roughly never, and a cycle
// should not spend an API round-trip re-learning them.

const canonTTL = 7 * 24 * time.Hour

type canonEntry struct {
	Canonical string `json:"canonical"`
	CheckedAt int64  `json:"checkedAt"`
}

func canonPath() string { return filepath.Join(Home(), "remotes.json") }

func loadCanonCache() map[string]canonEntry {
	out := map[string]canonEntry{}
	data, err := os.ReadFile(canonPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func saveCanonCache(c map[string]canonEntry) {
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return
	}
	if data, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(canonPath(), data, 0o644)
	}
}

// resolveRemotes returns canonical "owner/name" for each input, following
// renames and transfers. Unresolvable entries map to themselves, so a private,
// deleted, or non-GitHub remote degrades to the literal value rather than
// disappearing.
func resolveRemotes(remotes []string) map[string]string {
	result := make(map[string]string, len(remotes))
	cache := loadCanonCache()
	now := time.Now()

	var stale []string
	for _, r := range remotes {
		if r == "" {
			continue
		}
		if e, ok := cache[strings.ToLower(r)]; ok && now.Sub(time.UnixMilli(e.CheckedAt)) < canonTTL {
			result[r] = e.Canonical
			continue
		}
		stale = append(stale, r)
	}
	if len(stale) == 0 {
		return result
	}
	sort.Strings(stale) // deterministic query, so the alias order is stable

	// One aliased GraphQL query for every unknown remote, rather than N calls.
	var b strings.Builder
	b.WriteString("query {")
	valid := make([]string, 0, len(stale))
	for _, r := range stale {
		owner, name, ok := strings.Cut(r, "/")
		if !ok || owner == "" || name == "" {
			result[r] = r
			continue
		}
		fmt.Fprintf(&b, " r%d: repository(owner: %q, name: %q) { nameWithOwner }", len(valid), owner, name)
		valid = append(valid, r)
	}
	b.WriteString(" }")

	if len(valid) == 0 {
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "api", "graphql", "-f", "query="+b.String()).Output()
	if err != nil {
		// Offline or unauthenticated: fall back to the literal remotes. The
		// table degrades to the old behaviour rather than failing.
		for _, r := range valid {
			result[r] = r
		}
		return result
	}

	// Partial failures are normal here — a deleted or private repo returns null
	// for its alias alongside valid data — so parse leniently.
	var parsed struct {
		Data map[string]*struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &parsed) != nil {
		for _, r := range valid {
			result[r] = r
		}
		return result
	}

	for i, r := range valid {
		canonical := r
		if node, ok := parsed.Data[fmt.Sprintf("r%d", i)]; ok && node != nil && node.NameWithOwner != "" {
			canonical = node.NameWithOwner
		}
		result[r] = canonical
		cache[strings.ToLower(r)] = canonEntry{Canonical: canonical, CheckedAt: now.UnixMilli()}
	}
	saveCanonCache(cache)
	return result
}

// CanonicalizeRepos rewrites each repo's Remote to its canonical form and
// reports which ones moved, so the change can be surfaced rather than silently
// applied.
func CanonicalizeRepos(repos []RepoSignal) map[string]string {
	remotes := make([]string, 0, len(repos))
	for _, r := range repos {
		if r.Remote != "" {
			remotes = append(remotes, r.Remote)
		}
	}
	resolved := resolveRemotes(remotes)

	moved := map[string]string{}
	for i := range repos {
		was := repos[i].Remote
		if was == "" {
			continue
		}
		if now, ok := resolved[was]; ok && now != was {
			moved[was] = now
			repos[i].Remote = now
		}
	}
	return moved
}
