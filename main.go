package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

func buildSearchQuery(author string) string {
	query := []string{
		"user:openshift",
		"label:approved",
		"label:lgtm",
		"-label:hold",
		"-label:needs-rebase",
		"state:open",
		fmt.Sprintf("author:%s", author),
	}
	return strings.Join(query, " ")
}

func getOwnerAndRepository(u string) (string, string) {
	parts := strings.Split(strings.TrimPrefix(u, "https://github.com/"), "/")
	return parts[0], parts[1]
}

func findLatestFailures(statuses []*github.RepoStatus) []*github.RepoStatus {
	m := map[string][]*github.RepoStatus{}
	for _, s := range statuses {
		ctx := s.GetContext()
		m[ctx] = append(m[ctx], s)
		sort.Slice(m[ctx], func(i, j int) bool {
			return m[ctx][j].GetUpdatedAt().Before(m[ctx][i].GetUpdatedAt())
		})
	}
	foundFailures := []*github.RepoStatus{}
	for s := range m {
		if m[s][0].GetState() == "failure" {
			foundFailures = append(foundFailures, m[s][0])
		}
	}
	return foundFailures
}

func main() {
	ctx := context.TODO()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	result, _, err := client.Search.Issues(ctx, buildSearchQuery(os.Getenv("GITHUB_USER")), &github.SearchOptions{Sort: "updated"})
	if err != nil {
		log.Fatal(err)
	}

	retestIssues := []github.Issue{}

	for _, issue := range result.Issues {
		owner, repo := getOwnerAndRepository(issue.GetHTMLURL())
		commits, _, err := client.PullRequests.ListCommits(ctx, owner, repo, issue.GetNumber(), &github.ListOptions{})
		if err != nil {
			log.Fatal(err)
		}
		lastCommit := commits[len(commits)-1]
		statuses, _, err := client.Repositories.ListStatuses(ctx, owner, repo, lastCommit.GetSHA(), &github.ListOptions{})
		if failures := findLatestFailures(statuses); len(failures) > 0 {
			fmt.Printf("[retest] %s - %s\n", repo, issue.GetTitle())
			for _, f := range failures {
				fmt.Printf("         FAILED: %s - %s\n", f.GetContext(), f.GetTargetURL())
			}
			retestIssues = append(retestIssues, issue)
		}
	}

	for _, issue := range retestIssues {
		owner, repo := getOwnerAndRepository(issue.GetHTMLURL())
		retestBody := "/retest"
		oneReaction := 1
		c, _, err := client.Issues.CreateComment(ctx, owner, repo, issue.GetNumber(), &github.IssueComment{
			Body: &retestBody,
			Reactions: &github.Reactions{
				Laugh: &oneReaction,
			},
		})
		if err != nil {
			log.Fatal("Failed to make comment on %s: %v", issue.GetHTMLURL(), err)
		}
		if _, _, err := client.Reactions.CreateCommentReaction(ctx, owner, repo, c.GetID(), "laugh"); err != nil {
			log.Printf("Can't make laugh on %s: %v", issue.GetHTMLURL(), err)
		}
	}
}
