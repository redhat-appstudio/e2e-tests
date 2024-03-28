package gitlab

import (
	gitlab "github.com/xanzy/go-gitlab"
)

type GitlabClient struct {
	client *gitlab.Client
}

func NewGitlabClient(accessToken, baseUrl string) (*GitlabClient, error) {

	var err error
	var glc = &GitlabClient{}

	glc.client, err = gitlab.NewClient(accessToken, gitlab.WithBaseURL(baseUrl))
	if err != nil {
		return nil, err
	}

	return glc, nil
}

// GetClient returns the underlying gitlab client
func (gc *GitlabClient) GetClient() *gitlab.Client {
	return gc.client
}
