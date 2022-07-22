package tekton

import (
	"fmt"
	"strings"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"knative.dev/pkg/apis"
)

type TaskRunResultMatcher struct {
	name        string
	value       *string
	jsonValue   *interface{}
	jsonMatcher types.GomegaMatcher
}

func (matcher *TaskRunResultMatcher) FailureMessage(actual interface{}) (message string) {
	if matcher.value != nil {
		return fmt.Sprintf("%v to equal %v", actual, v1beta1.TaskRunResult{
			Name:  matcher.name,
			Value: *matcher.value,
		})
	}

	return matcher.jsonMatcher.FailureMessage(actual)
}

func (matcher *TaskRunResultMatcher) Match(actual interface{}) (success bool, err error) {
	if tr, ok := actual.(v1beta1.TaskRunResult); !ok {
		return false, fmt.Errorf("not given TaskRunResult")
	} else {
		if tr.Name != matcher.name {
			return false, nil
		}

		if matcher.value != nil {
			return strings.TrimSpace(tr.Value) == *matcher.value, nil
		} else {
			matcher.jsonMatcher = gomega.MatchJSON(*matcher.jsonValue)
			return matcher.jsonMatcher.Match(tr.Value)
		}
	}
}

func (matcher *TaskRunResultMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	if matcher.value != nil {
		return fmt.Sprintf("%v not to equal %v", actual, v1beta1.TaskRunResult{
			Name:  matcher.name,
			Value: strings.TrimSpace(*matcher.value),
		})
	}

	return matcher.jsonMatcher.NegatedFailureMessage(actual)
}

func MatchTaskRunResult(name, value string) types.GomegaMatcher {
	return &TaskRunResultMatcher{name: name, value: &value}
}

func MatchTaskRunResultWithJSONValue(name string, json interface{}) types.GomegaMatcher {
	return &TaskRunResultMatcher{name: name, jsonValue: &json}
}

func DidTaskSucceed(tr interface{}) bool {
	switch tr := tr.(type) {
	case *v1beta1.PipelineRunTaskRunStatus:
		return tr.Status.GetCondition(apis.ConditionSucceeded).IsTrue()
	case *v1beta1.TaskRunStatus:
		return tr.Status.GetCondition(apis.ConditionSucceeded).IsTrue()
	}
	return false
}
