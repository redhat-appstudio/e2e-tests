package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/magefile/mage/sh"
)

func getRemoteAndBranchNameFromPRLink(url string) (remote, branchName string, err error) {
	ghRes := &GithubPRInfo{}
	if err := sendHttpRequestAndParseResponse(url, "GET", ghRes); err != nil {
		return "", "", err
	}

	split := strings.Split(ghRes.Head.Label, ":")
	remote, branchName = split[0], split[1]

	return remote, branchName, nil
}

func gitCheckoutRemoteBranch(remoteName, branchName string) error {
	var git = sh.RunCmd("git")
	for _, arg := range [][]string{
		{"remote", "add", remoteName, fmt.Sprintf("https://github.com/%s/e2e-tests.git", remoteName)},
		{"fetch", remoteName},
		{"checkout", branchName},
	} {
		if err := git(arg...); err != nil {
			return fmt.Errorf("error when checkout out remote branch %s from remote %s: %v", branchName, remoteName, err)
		}
	}
	return nil
}

func sendHttpRequestAndParseResponse(url, method string, v interface{}) error {
	req, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("token %s", os.Getenv("GITHUB_TOKEN")))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return err
	}

	return nil
}
