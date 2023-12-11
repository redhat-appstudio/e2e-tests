package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/devfile/library/v2/pkg/util"
	. "github.com/onsi/ginkgo/v2"
	appstudioApi "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/logs"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	intgteststat "github.com/redhat-appstudio/integration-service/pkg/integrationteststatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SnapshotTestsStatusAnnotation is annotation in snapshot where integration test results are stored
const SnapshotTestsStatusAnnotation = "test.appstudio.openshift.io/status"

// CreateSnapshotWithComponents creates a Snapshot using the given parameters.
func (i *IntegrationController) CreateSnapshotWithComponents(snapshotName, componentName, applicationName, namespace string, snapshotComponents []appstudioApi.SnapshotComponent) (*appstudioApi.Snapshot, error) {
	snapshot := &appstudioApi.Snapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: namespace,
			Labels: map[string]string{
				"test.appstudio.openshift.io/type":           "component",
				"appstudio.openshift.io/component":           componentName,
				"pac.test.appstudio.openshift.io/event-type": "push",
			},
		},
		Spec: appstudioApi.SnapshotSpec{
			Application: applicationName,
			Components:  snapshotComponents,
		},
	}
	return snapshot, i.KubeRest().Create(context.Background(), snapshot)
}

// CreateSnapshotWithImage creates a snapshot using an image.
func (i *IntegrationController) CreateSnapshotWithImage(componentName, applicationName, namespace, containerImage string) (*appstudioApi.Snapshot, error) {
	snapshotComponents := []appstudioApi.SnapshotComponent{
		{
			Name:           componentName,
			ContainerImage: containerImage,
		},
	}

	snapshotName := "snapshot-sample-" + util.GenerateRandomString(4)

	return i.CreateSnapshotWithComponents(snapshotName, componentName, applicationName, namespace, snapshotComponents)
}

// GetSnapshotByComponent returns the first snapshot in namespace if exist, else will return nil
func (i *IntegrationController) GetSnapshotByComponent(namespace string) (*appstudioApi.Snapshot, error) {
	snapshot := &appstudioApi.SnapshotList{}
	opts := []client.ListOption{
		client.MatchingLabels{
			"test.appstudio.openshift.io/type": "component",
		},
		client.InNamespace(namespace),
	}
	err := i.KubeRest().List(context.Background(), snapshot, opts...)

	if err == nil && len(snapshot.Items) > 0 {
		return &snapshot.Items[0], nil
	}
	return nil, err
}

// GetSnapshot returns the Snapshot in the namespace and nil if it's not found
// It will search for the Snapshot based on the Snapshot name, associated PipelineRun name or Component name
// In the case the List operation fails, an error will be returned.
func (i *IntegrationController) GetSnapshot(snapshotName, pipelineRunName, componentName, namespace string) (*appstudioApi.Snapshot, error) {
	ctx := context.Background()
	// If Snapshot name is provided, try to get the resource directly
	if len(snapshotName) > 0 {
		snapshot := &appstudioApi.Snapshot{}
		if err := i.KubeRest().Get(ctx, types.NamespacedName{Name: snapshotName, Namespace: namespace}, snapshot); err != nil {
			return nil, fmt.Errorf("couldn't find Snapshot with name '%s' in '%s' namespace", snapshotName, namespace)
		}
		return snapshot, nil
	}
	// Search for the Snapshot in the namespace based on the associated Component or PipelineRun
	snapshots := &appstudioApi.SnapshotList{}
	opts := []client.ListOption{
		client.InNamespace(namespace),
	}
	err := i.KubeRest().List(ctx, snapshots, opts...)
	if err != nil {
		return nil, fmt.Errorf("error when listing Snapshots in '%s' namespace: %v", namespace, err)
	}

	if snapshots.Items == nil || len(snapshots.Items) == 0 {
		return nil, fmt.Errorf("error when fetching Snapshots in '%s' namespace: The Items field is either uninitialized or contains zero elements", namespace)
	}

	for _, snapshot := range snapshots.Items {
		if snapshot.Name == snapshotName {
			return &snapshot, nil
		}
		// find snapshot by pipelinerun name
		if len(pipelineRunName) > 0 && snapshot.Labels["appstudio.openshift.io/build-pipelinerun"] == pipelineRunName {
			return &snapshot, nil

		} else {
			klog.Infof("build-pipelinerun - %s, pipelineRunName - %s", snapshot.Labels["appstudio.openshift.io/build-pipelinerun"], pipelineRunName)
		}
		// find snapshot by component name
		if len(componentName) > 0 && snapshot.Labels["appstudio.openshift.io/component"] == componentName {
			return &snapshot, nil

		}
	}
	return nil, fmt.Errorf("no snapshot found for component '%s', pipelineRun '%s' in '%s' namespace", componentName, pipelineRunName, namespace)
}

// DeleteSnapshot removes given snapshot from specified namespace.
func (i *IntegrationController) DeleteSnapshot(hasSnapshot *appstudioApi.Snapshot, namespace string) error {
	err := i.KubeRest().Delete(context.Background(), hasSnapshot)
	return err
}

// DeleteAllSnapshotsInASpecificNamespace removes all snapshots from a specific namespace. Useful when creating a lot of resources and want to remove all of them
func (i *IntegrationController) DeleteAllSnapshotsInASpecificNamespace(namespace string, timeout time.Duration) error {
	if err := i.KubeRest().DeleteAllOf(context.Background(), &appstudioApi.Snapshot{}, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("error deleting snapshots from the namespace %s: %+v", namespace, err)
	}

	return utils.WaitUntil(func() (done bool, err error) {
		snapshotList, err := i.ListAllSnapshots(namespace)
		if err != nil {
			return false, nil
		}
		return len(snapshotList.Items) == 0, nil
	}, timeout)
}

// WaitForSnapshotToGetCreated wait for the Snapshot to get created successfully.
func (i *IntegrationController) WaitForSnapshotToGetCreated(snapshotName, pipelinerunName, componentName, testNamespace string) (*appstudioApi.Snapshot, error) {
	var snapshot *appstudioApi.Snapshot

	err := wait.PollUntilContextTimeout(context.Background(), constants.PipelineRunPollingInterval, 10*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		snapshot, err = i.GetSnapshot(snapshotName, pipelinerunName, componentName, testNamespace)
		if err != nil {
			GinkgoWriter.Printf("unable to get the Snapshot within the namespace %s. Error: %v", testNamespace, err)
			return false, nil
		}

		return true, nil
	})

	return snapshot, err
}

// ListAllSnapshots returns a list of all Snapshots in a given namespace.
func (i *IntegrationController) ListAllSnapshots(namespace string) (*appstudioApi.SnapshotList, error) {
	snapshotList := &appstudioApi.SnapshotList{}
	err := i.KubeRest().List(context.Background(), snapshotList, &client.ListOptions{Namespace: namespace})

	return snapshotList, err
}

// StoreSnapshot stores a given Snapshot as an artifact.
func (i *IntegrationController) StoreSnapshot(snapshot *appstudioApi.Snapshot) error {
	return logs.StoreResourceYaml(snapshot, "snapshot-"+snapshot.Name)
}

// StoreAllSnapshots stores all Snapshots in a given namespace.
func (i *IntegrationController) StoreAllSnapshots(namespace string) error {
	snapshotList, err := i.ListAllSnapshots(namespace)
	if err != nil {
		return err
	}

	for _, snapshot := range snapshotList.Items {
		if err := i.StoreSnapshot(&snapshot); err != nil {
			return err
		}
	}
	return nil
}

// GetIntegrationTestStatusDetailFromSnapshot parses snapshot annotation and returns integration test status detail
func (i *IntegrationController) GetIntegrationTestStatusDetailFromSnapshot(snapshot *appstudioApi.Snapshot, scenarioName string) (*intgteststat.IntegrationTestStatusDetail, error) {
	var (
		resultsJson string
		ok          bool
	)
	annotations := snapshot.GetAnnotations()
	resultsJson, ok = annotations[SnapshotTestsStatusAnnotation]
	if !ok {
		resultsJson = ""
	}
	statuses, err := intgteststat.NewSnapshotIntegrationTestStatuses(resultsJson)
	if err != nil {
		return nil, fmt.Errorf("failed to create new SnapshotIntegrationTestStatuses object: %w", err)
	}
	statusDetail, ok := statuses.GetScenarioStatus(scenarioName)
	if !ok {
		return nil, fmt.Errorf("status detail for scenario %s not found", scenarioName)
	}
	return statusDetail, nil
}
