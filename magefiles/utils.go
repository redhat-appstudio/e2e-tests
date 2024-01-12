package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"k8s.io/klog/v2"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	plumbingHttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	sprig "github.com/go-task/slim-sprig"
	"github.com/magefile/mage/sh"
	"github.com/redhat-appstudio/e2e-tests/pkg/clients/slack"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/image-controller/pkg/quay"
)

const quayPrefixesToDeleteRegexp = "appstudio-e2e|build-e2e|byoc-|e2e[-_](demos|pyxis)|has-e2e|integration-e2e|jvm[_-]build|multi-comp|mvp[_-]dev|release-(default|deploy|dev|e2e)|rhtap[-_]demo|stackcode|user1-"

func getRemoteAndBranchNameFromPRLink(url string) (remote, branchName string, err error) {
	ghRes := &GithubPRInfo{}
	if err := sendHttpRequestAndParseResponse(url, "GET", ghRes); err != nil {
		return "", "", err
	}

	if ghRes.Head.Label == "" {
		return "", "", fmt.Errorf("failed to get an information about the remote and branch name from PR %s", url)
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
		return fmt.Errorf("error when sending request to '%s': %+v", url, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("error when reading the response body from URL '%s': %+v", url, err)
	}
	if res.StatusCode > 299 {
		return fmt.Errorf("unexpected status code: %d, response body: %s", res.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("error when unmarshalling the response body from URL '%s': %+v", url, err)
	}

	return nil
}

func retry(f func() error, attempts int, delay time.Duration) error {
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			klog.Infof("got an error: %+v - will retry in %v", err, delay)
			time.Sleep(delay)
		}
		err = f()
		if err != nil {
			continue
		} else {
			return nil
		}
	}
	return fmt.Errorf("reached maximum number of attempts (%d). error: %+v", attempts, err)
}

func goFmt(path string) error {
	err := sh.RunV("go", "fmt", path)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("Could not fmt:\n%s\n", path), err)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func renderTemplate(destination, templatePath string, templateData interface{}, appendDestination bool) error {

	var templateText string
	var f *os.File
	var err error

	/* This decision logic feels a little clunky cause initially I wanted to
	to have this func create the new file and render the template into the new
	file. But with the updating the pkg/framework/describe.go use case
	I wanted to reuse leveraging the txt/template package rather than
	rendering/updating using strings/regex.
	*/
	if appendDestination {

		f, err = os.OpenFile(destination, os.O_APPEND|os.O_WRONLY, 0664)
		if err != nil {
			klog.Infof("Failed to open file: %v", err)
		}
	} else {

		if fileExists(destination) {
			return fmt.Errorf("%s already exists", destination)
		}
		f, err = os.Create(destination)
		if err != nil {
			klog.Infof("Failed to create file: %v", err)
		}
	}

	defer f.Close()

	tpl, err := os.ReadFile(templatePath)
	if err != nil {
		klog.Infof("error reading file: %v", err)

	}
	var tmplText = string(tpl)
	templateText = fmt.Sprintf("\n%s", tmplText)
	specTemplate, err := template.New("spec").Funcs(sprig.TxtFuncMap()).Parse(templateText)
	if err != nil {
		klog.Infof("error parsing template file: %v", err)

	}

	err = specTemplate.Execute(f, templateData)
	if err != nil {
		klog.Infof("error rendering template file: %v", err)
	}

	return nil
}

func cleanupQuayReposAndRobots(quayService quay.QuayService, quayOrg string) error {
	repoRegExp, err := regexp.Compile(fmt.Sprintf(`^(%s)`, quayPrefixesToDeleteRegexp))
	if err != nil {
		return err
	}

	repos, err := quayService.GetAllRepositories(quayOrg)
	if err != nil {
		return err
	}

	// Key is the repo name without slashes which is the same as robot name
	// Value is the repo name with slashes
	reposMap := make(map[string]string)

	for _, repo := range repos {
		if repoRegExp.MatchString(repo.Name) {
			sanitizedRepoName := strings.ReplaceAll(repo.Name, "/", "") // repo name without slashes
			reposMap[sanitizedRepoName] = repo.Name
		}
	}

	robots, err := quayService.GetAllRobotAccounts(quayOrg)
	if err != nil {
		return err
	}

	robotRegExp, err := regexp.Compile(fmt.Sprintf(`^%s\+(%s)`, quayOrg, quayPrefixesToDeleteRegexp))
	if err != nil {
		return err
	}

	const timeFormat = "Mon, 02 Jan 2006 15:04:05 -0700"

	// Deletes robots and their repos with correct prefix if created more than 24 hours ago
	for _, robot := range robots {
		parsed, err := time.Parse(timeFormat, robot.Created)
		if err != nil {
			return err
		}

		// If robot.Name has correct prefix and was created more than 24 hours ago
		if robotRegExp.MatchString(robot.Name) && time.Since(parsed) > 24*time.Hour {
			// Robot name without the name of org which is the same as previous sanitizedRepoName
			// redhat-appstudio-qe+e2e-demos turns to e2e-demos
			splitRobotName := strings.Split(robot.Name, "+")
			if len(splitRobotName) != 2 {
				return fmt.Errorf("failed to split robot name into 2 parts, got %d parts", len(splitRobotName))
			}
			sanitizedRepoName := splitRobotName[1] // Same as robot shortname
			if repo, exists := reposMap[sanitizedRepoName]; exists {
				klog.Infof("deleting repository %s", repo)
				deleted, err := quayService.DeleteRepository(quayOrg, repo)
				if err != nil {
					return fmt.Errorf("failed to delete repository %s, error: %s", repo, err)
				}
				if !deleted {
					fmt.Printf("repository %s has already been deleted, skipping\n", repo)
				}
			}
			// DeleteRobotAccount uses robot shortname, so e2e-demos instead of redhat-appstudio-qe+e2e-demos
			klog.Infof("deleting robot account %s", splitRobotName[1])
			deleted, err := quayService.DeleteRobotAccount(quayOrg, splitRobotName[1])
			if err != nil {
				return fmt.Errorf("failed to delete robot account %s, error: %s", robot.Name, err)
			}
			if !deleted {
				fmt.Printf("robot account %s has already been deleted, skipping\n", robot.Name)
			}
		}
	}
	// Additional cleanup - orphaned repositories (no associated robot account), last updated 24 hours ago
	for _, repo := range repos {
		updated := time.Since(time.Unix(int64(repo.LastModified), 0))
		if (repoRegExp.MatchString(repo.Name)) && updated > 24*time.Hour {
			klog.Infof("deleting repository %s", repo.Name)
			_, err := quayService.DeleteRepository(quayOrg, repo.Name)
			if err != nil {
				return fmt.Errorf("failed to delete repository %s, error: %s", repo.Name, err)
			}
		}
	}
	return nil
}

func cleanupQuayTags(quayService quay.QuayService, organization, repository string) error {
	workerCount := 10
	var wg sync.WaitGroup

	var allTags []quay.Tag
	var errors []error

	page := 1000
	for {
		tags, hasAdditional, err := quayService.GetTagsFromPage(organization, repository, page)
		klog.Infof("page: %d, num of tags: %d, num of errors: %d", page, len(allTags), len(errors))
		page++
		if err != nil {
			errors = append(errors, fmt.Errorf("error getting tags of `%s` repository of `%s` organization on page `%d`, error: %s", repository, organization, page, err))
			continue
		}
		allTags = append(allTags, tags...)
		if !hasAdditional {
			break
		}
	}

	wg.Add(workerCount)

	var errorsMutex sync.Mutex
	for i := 0; i < workerCount; i++ {
		go func(startIdx int, allTags []quay.Tag, errors []error, errorsMutex *sync.Mutex, wg *sync.WaitGroup) {
			defer wg.Done()
			for idx := startIdx; idx < len(allTags); idx += workerCount {
				tag := allTags[idx]
				if time.Unix(tag.StartTS, 0).Before(time.Now().AddDate(0, 0, -7)) {
					klog.Infof("deleting %s", tag.Name)
					deleted, err := quayService.DeleteTag(organization, repository, tag.Name)
					if err != nil {
						klog.Infof("error when deleting %s: %+v", tag.Name, err)
						errorsMutex.Lock()
						errors = append(errors, fmt.Errorf("error during deletion of tag `%s` in repository `%s` of organization `%s`, error: `%s`", tag.Name, repository, organization, err))
						errorsMutex.Unlock()
					} else if !deleted {
						fmt.Printf("tag `%s` in repository `%s` of organization `%s` was not deleted\n", tag.Name, repository, organization)
					}
				}
			}
		}(i, allTags, errors, &errorsMutex, &wg)
	}

	wg.Wait()

	if len(errors) == 0 {
		return nil
	}

	var errBuilder strings.Builder
	for _, err := range errors {
		errBuilder.WriteString(fmt.Sprintf("%s\n", err))
	}
	return fmt.Errorf("encountered errors during CleanupQuayTags: %s", errBuilder.String())
}

func repoNameStartsWithPrefix(prefixes []string, repoName string) bool {
	for _, prefix := range prefixes {
		hasPrefix := strings.HasPrefix(repoName, prefix)
		if hasPrefix {
			return true
		}
	}
	return false
}

// Deletes the private repos older than 7 days
func cleanupPrivateRepos(quayService quay.QuayService, quayOrg string, repoNamePrefixes []string) error {
	repos, err := quayService.GetAllRepositories(quayOrg)
	if err != nil {
		return err
	}
	foundError := false
	var errors []error
	for _, repo := range repos {
		if !repo.IsPublic && repoNameStartsWithPrefix(repoNamePrefixes, repo.Name) {
			lastModifiedTime := int64(repo.LastModified)
			currentTime := time.Now().Unix()
			timeDiff := int64(7 * 24 * 3600)
			if currentTime-lastModifiedTime > timeDiff {
				_, err := quayService.DeleteRepository(quayOrg, repo.Name)
				if err != nil {
					foundError = true
					errors = append(errors, fmt.Errorf("failed to delete repository %s with error: %v", repo.Name, err))
					continue
				}
				fmt.Printf("Deleted repo %s successfully\n", repo.Name)
			}
		}
	}
	if foundError {
		var errBuilder strings.Builder
		for _, err := range errors {
			errBuilder.WriteString(fmt.Sprintf("%s\n", err))
		}
		return fmt.Errorf("encountered errors during cleanup of private repos: %s", errBuilder.String())
	}
	return nil
}

func MergePRInRemote(branch string, forkOrganization string, repoPath string) error {
	if branch == "" {
		klog.Fatal("The branch for upgrade is empty!")
	}
	var auth = &plumbingHttp.BasicAuth{
		Username: "123",
		Password: utils.GetEnv("GITHUB_TOKEN", ""),
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		klog.Fatal(err)
	}

	branches, err := repo.Branches()
	if err != nil {
		klog.Fatal(err)
	}

	var previewBranchRef *plumbing.Reference
	err = branches.ForEach(func(ref *plumbing.Reference) error {
		if !strings.Contains("main", ref.Name().String()) {
			previewBranchRef = ref
		}
		return nil
	})
	if err != nil {
		klog.Fatal(err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		klog.Fatal(err)
	}

	err = wt.Checkout(&git.CheckoutOptions{
		Branch: previewBranchRef.Name(),
	})
	if err != nil {
		klog.Fatal(err)
	}

	if forkOrganization == "redhat-appstudio" {
		// Cloned repository have as origin set redhat-appstudio organization
		err = mergeBranch(repoPath, "remotes/origin/"+branch)
	} else {
		repoURL := fmt.Sprintf("https://github.com/%s/infra-deployments.git", forkOrganization)
		_, err = repo.CreateRemote(&config.RemoteConfig{
			Name: "forked_repo",
			URLs: []string{repoURL},
		})
		if err != nil {
			klog.Fatal(err)
		}

		err = repo.Fetch(&git.FetchOptions{
			RemoteName: "forked_repo",
		})
		if err != nil {
			klog.Fatal(err)
		}
		err = mergeBranch(repoPath, "remotes/forked_repo/"+branch)
	}
	if err != nil {
		klog.Fatal(err)
	}

	err = repo.Push(&git.PushOptions{
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("%s:%s", previewBranchRef.Name().String(), previewBranchRef.Name().String()))},
		RemoteName: "qe",
		Auth:       auth,
	})
	if err != nil {
		klog.Fatal(err)
	}

	return nil
}

func mergeBranch(repoPath string, branchToMerge string) error {
	_, err := exec.Command("git", "-C", repoPath, "merge", branchToMerge, "-Xtheirs", "-q").Output()
	if err != nil {
		klog.Fatal(err)
	}
	return nil
}

func HandleErrorWithAlert(err error, errLevel slack.ErrorSeverityLevel) error {
	klog.Warning(err.Error() + " - this issue will be reported to a dedicated Slack channel")

	if slackErr := slack.ReportIssue(err.Error(), errLevel); slackErr != nil {
		return fmt.Errorf("failed report an error (%s) to a Slack channel: %s", err, slackErr)
	}
	return nil
}
