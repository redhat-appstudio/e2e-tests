package main

import "github.com/magefile/mage/mg"

type Local mg.Namespace
type CI mg.Namespace

type OpenshiftJobSpec struct {
	Refs Refs `json:"refs"`
}
type Refs struct {
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

type GithubBranches struct {
	Branches []Branch
}

type Branch struct {
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
