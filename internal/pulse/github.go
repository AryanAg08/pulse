package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// One GraphQL round-trip for both lists. Auth comes from the `gh` CLI, so Pulse
// never handles a token or needs its own OAuth app.
const prQuery = `
query($mine: String!, $reviews: String!) {
  mine: search(query: $mine, type: ISSUE, first: 25) { nodes { ...pr } }
  reviews: search(query: $reviews, type: ISSUE, first: 25) { nodes { ...pr } }
}
fragment pr on PullRequest {
  number
  title
  url
  isDraft
  createdAt
  updatedAt
  reviewDecision
  author { login }
  repository { nameWithOwner }
  commits(last: 1) {
    nodes { commit { statusCheckRollup { state } } }
  }
}`

type gqlPR struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	IsDraft        bool   `json:"isDraft"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	ReviewDecision string `json:"reviewDecision"`
	Author         *struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

type gqlResponse struct {
	Data struct {
		Mine    struct{ Nodes []gqlPR } `json:"mine"`
		Reviews struct{ Nodes []gqlPR } `json:"reviews"`
	} `json:"data"`
}

func (p gqlPR) toSignal() PRSignal {
	checks := "none"
	if len(p.Commits.Nodes) > 0 {
		if r := p.Commits.Nodes[0].Commit.StatusCheckRollup; r != nil {
			switch r.State {
			case "SUCCESS":
				checks = "passing"
			case "FAILURE", "ERROR":
				checks = "failing"
			case "PENDING", "EXPECTED":
				checks = "pending"
			}
		}
	}
	author := "unknown"
	if p.Author != nil {
		author = p.Author.Login
	}
	stale := 0.0
	if t, err := time.Parse(time.RFC3339, p.UpdatedAt); err == nil {
		stale = time.Since(t).Hours()
	}
	return PRSignal{
		Repo:           p.Repository.NameWithOwner,
		Number:         p.Number,
		Title:          p.Title,
		URL:            p.URL,
		IsDraft:        p.IsDraft,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
		StaleHours:     stale,
		ReviewDecision: p.ReviewDecision,
		Checks:         checks,
		Author:         author,
	}
}

type GithubResult struct {
	PRs            []PRSignal
	ReviewRequests []PRSignal
	OK             bool
	Err            string
}

// CollectGithub never returns an error: GitHub being unreachable must not break
// a cycle, because the local git signals still work without it.
func CollectGithub(login string) GithubResult {
	who := login
	if who == "" {
		who = "@me"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-f", "query="+prQuery,
		"-F", fmt.Sprintf("mine=is:pr is:open archived:false author:%s", who),
		"-F", fmt.Sprintf("reviews=is:pr is:open archived:false review-requested:%s", who),
	)
	out, err := cmd.Output()
	if err != nil {
		return GithubResult{OK: false, Err: truncate(err.Error(), 200)}
	}

	var parsed gqlResponse
	if err := json.Unmarshal(out, &parsed); err != nil {
		return GithubResult{OK: false, Err: truncate(err.Error(), 200)}
	}

	res := GithubResult{OK: true}
	for _, n := range parsed.Data.Mine.Nodes {
		res.PRs = append(res.PRs, n.toSignal())
	}
	for _, n := range parsed.Data.Reviews.Nodes {
		res.ReviewRequests = append(res.ReviewRequests, n.toSignal())
	}
	return res
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
