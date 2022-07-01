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
	sendHttpRequestAndParseResponse(url, "GET", ghRes)

	split := strings.Split(ghRes.Head.Label, ":")
	remote, branchName = split[0], split[1]

	return remote, branchName, nil
}

func gitCheckoutRemoteBranch(remoteName, branchName string) {
	sh.Run("git", "remote", "add", remoteName, fmt.Sprintf("https://github.com/%s/e2e-tests.git", remoteName))
	sh.Run("git", "fetch", remoteName)
	sh.Run("git", "checkout", branchName)
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
