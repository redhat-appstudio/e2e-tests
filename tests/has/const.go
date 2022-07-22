package has

const (
	RedHatAppStudioApplicationName string = "pet-clinic-e2e"

	// Argo CD Application service name: https://github.com/redhat-appstudio/infra-deployments/blob/main/argo-cd-apps/base/has.yaml#L4
	HASArgoApplicationName string = "has"

	// Application Service controller is deployed the namespace: https://github.com/redhat-appstudio/infra-deployments/blob/main/argo-cd-apps/base/has.yaml#L14
	RedHatAppStudioApplicationNamespace string = "application-service"

	// Red Hat AppStudio ArgoCD Applications are created in 'openshift-gitops' namespace. See: https://github.com/redhat-appstudio/infra-deployments/blob/main/argo-cd-apps/app-of-apps/all-applications-staging.yaml#L5
	GitOpsNamespace string = "openshift-gitops"

	// Component name used with quarkus devfile.
	QuarkusComponentName string = "quarkus-component-e2e"

	// Sample devfile created redhat-appstudio-qe repository with the following content:
	QuarkusDevfileSource string = "https://github.com/redhat-appstudio-qe/devfile-sample-code-with-quarkus"

	// The default private devfile sample to use if none is passed in via the PRIVATE_DEVFILE_SAMPLE env variable.
	PrivateQuarkusDevfileSource string = "https://github.com/redhat-appstudio-qe/private-quarkus-devfile-sample"

	// See more info: https://github.com/redhat-appstudio/application-service#creating-a-github-secret-for-has
	ApplicationServiceGHTokenSecrName string = "has-github-token" // #nosec

	// Name for the GitOps Deployment resource
	GitOpsDeploymentName string = "gitops-deployment-e2e"

	// GitOps repository branch to use
	GitOpsRepositoryRevision string = "main"
)
