package e2e

const (
	// Argo CD Application service name: https://github.com/redhat-appstudio/infra-deployments/blob/main/argo-cd-apps/base/has.yaml#L4
	HASArgoApplicationName string = "has"

	// Application Service controller is deployed the namespace: https://github.com/redhat-appstudio/infra-deployments/blob/main/argo-cd-apps/base/has.yaml#L14
	RedHatAppStudioApplicationNamespace string = "application-service"

	// Red Hat AppStudio ArgoCD Applications are created in 'openshift-gitops' namespace. See: https://github.com/redhat-appstudio/infra-deployments/blob/main/argo-cd-apps/app-of-apps/all-applications-staging.yaml#L5
	GitOpsNamespace string = "openshift-gitops"

	// See more info: https://github.com/redhat-appstudio/application-service#creating-a-github-secret-for-has
	ApplicationServiceGHTokenSecrName string = "has-github-token" // #nosec

	// Name for the GitOps Deployment resource
	GitOpsDeploymentName string = "gitops-deployment-e2e"

	// GitOps repository branch to use
	GitOpsRepositoryRevision string = "main"

	// Component deployment replicas
	ComponentDeploymentReplicas int = 3
)
