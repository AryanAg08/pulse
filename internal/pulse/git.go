package pulse

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// git runs a git subcommand in repo, returning trimmed stdout. Errors collapse
// to an empty string: a missing upstream or an empty log is normal, not fatal.
func git(repo string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	full := append([]string{"-C", repo}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "vendor": true,
	"dist": true, "build": true, "target": true, "Library": true,
}

// DiscoverRepos walks each root looking for .git, stopping at maxDepth so a
// deep tree can't stall a cycle.
func DiscoverRepos(roots []string, maxDepth int) []string {
	var found []string

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			found = append(found, dir)
			return // don't descend into a repo looking for nested repos
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || skipDirs[e.Name()] {
				continue
			}
			walk(filepath.Join(dir, e.Name()), depth+1)
		}
	}

	for _, root := range roots {
		if _, err := os.Stat(root); err == nil {
			walk(root, 0)
		}
	}
	return found
}

// lastEditMs is the newest mtime among files git considers modified — the best
// available proxy for "typing right now". Returns -1 when nothing is modified.
func lastEditMs(repo string) int64 {
	out := git(repo, "status", "--porcelain", "-uall")
	if out == "" {
		return -1
	}
	var newest int64 = -1
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		file := strings.TrimSpace(line[3:])
		if file == "" {
			continue
		}
		st, err := os.Stat(filepath.Join(repo, file))
		if err != nil {
			continue // deleted or renamed — no mtime to read
		}
		if ms := st.ModTime().UnixMilli(); ms > newest {
			newest = ms
		}
	}
	return newest
}

func ReadRepo(repo, githubLogin string) RepoSignal {
	branch := git(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		branch = "HEAD"
	}

	// Last commit by this user, not by anyone — a teammate's merge isn't your activity.
	logArgs := []string{"log", "-1", "--format=%ct"}
	if githubLogin != "" {
		logArgs = append(logArgs, "--author", githubLogin)
	}
	var msSinceCommit int64 = -1
	if epoch := git(repo, logArgs...); epoch != "" {
		if sec, err := strconv.ParseInt(epoch, 10, 64); err == nil {
			msSinceCommit = time.Now().UnixMilli() - sec*1000
		}
	}

	status := git(repo, "status", "--porcelain", "-uall")
	dirtyFiles := 0
	if status != "" {
		for _, l := range strings.Split(status, "\n") {
			if strings.TrimSpace(l) != "" {
				dirtyFiles++
			}
		}
	}

	// Staged and unstaged, so a fully-staged big change still counts.
	dirtyLines := 0
	for _, args := range [][]string{{"diff", "--numstat"}, {"diff", "--numstat", "--cached"}} {
		for _, line := range strings.Split(git(repo, args...), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			add, _ := strconv.Atoi(fields[0]) // "-" for binary files parses to 0
			del, _ := strconv.Atoi(fields[1])
			dirtyLines += add + del
		}
	}

	ahead, _ := strconv.Atoi(git(repo, "rev-list", "--count", "@{upstream}..HEAD"))

	edit := lastEditMs(repo)
	msSinceEdit := int64(-1)
	if edit > 0 {
		msSinceEdit = time.Now().UnixMilli() - edit
	}

	return RepoSignal{
		Name:              filepath.Base(repo),
		Path:              repo,
		Branch:            branch,
		MsSinceLastCommit: msSinceCommit,
		MsSinceLastEdit:   msSinceEdit,
		DirtyFiles:        dirtyFiles,
		DirtyLines:        dirtyLines,
		AheadOfRemote:     ahead,
	}
}

func CollectRepos(roots []string, githubLogin string) []RepoSignal {
	repos := DiscoverRepos(roots, 3)
	out := make([]RepoSignal, 0, len(repos))
	for _, r := range repos {
		out = append(out, ReadRepo(r, githubLogin))
	}
	return out
}
