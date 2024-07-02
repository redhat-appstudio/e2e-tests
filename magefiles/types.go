package main

import "github.com/magefile/mage/mg"

type Local mg.Namespace
type CI mg.Namespace

type OpenshiftJobSpec struct {
	Refs Refs `json:"refs"`
}
type Refs struct {
	RepoLink     string `json:"repo_link"`
	Repo         string `json:"repo"`
	Organization string `json:"org"`
	Pulls        []Pull `json:"pulls"`
}

type Pull struct {
	Number     int    `json:"number"`
	Author     string `json:"author"`
	SHA        string `json:"sha"`
	PRLink     string `json:"link"`
	AuthorLink string `json:"author_link"`
}

type GithubPRInfo struct {
	Head Head `json:"head"`
}

type Head struct {
	Label string `json:"label"`
}

type GithubBranch struct {
	Name string `json:"name"`
}

type PullRequestMetadata struct {
	Author       string
	Organization string
	RepoName     string
	BranchName   string
	CommitSHA    string
	Number       int
	RemoteName   string
}

// KonfluxCISpec contains metadata about a job in Konflux.
type KonfluxCISpec struct {
	// ContainerImage holds the image obtained from Konflux Integration Service Snapshot.
	ContainerImage string `json:"container_image"`

	// KonfluxComponent specifies the name of the Konflux component to which the job belongs.
	KonfluxComponent string `json:"konflux_component"`

	// KonfluxGitRefs holds data related to a pull request or push event in Konflux.
	KonfluxGitRefs KonfluxGitRefs `json:"git"`
}

// KonfluxGitRefs holds references to Git-related data for a Konflux job.
type KonfluxGitRefs struct {
	// PullRequestNumber represents the number associated with a pull request.
	PullRequestNumber int `json:"pull_request_number,omitempty"`

	// PullRequestAuthor represents the author of the pull request.
	PullRequestAuthor string `json:"pull_request_author,omitempty"`

	// GitOrg represents the organization in which the Git repository resides.
	GitOrg string `json:"git_org"`

	// GitRepo represents the name of the Git repository.
	GitRepo string `json:"git_repo"`

	// CommitSha represents the SHA of the commit associated with the event.
	CommitSha string `json:"commit_sha"`

	// EventType represents the type of event (e.g., pull request, push).
	EventType string `json:"event_type"`
}
