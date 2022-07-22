package config

// Define the basic specs for the configuration
type WorkflowSpec struct {
	// All tests configurations
	Tests []TestSpec `yaml:"tests"`
}

// Set of tests to run in appstudio
type TestSpec struct {
	// The test name corresponding to an application
	Name string `yaml:"name"`
	// Name of the application created in the cluster
	ApplicationName string `yaml:"applicationName"`
	// Set of components with own specs
	Components []ComponentSpec `yaml:"components"`
}

// Set k8s resource specific properties
type K8sSpec struct {
	// If set, will scale the replicas to the desired number
	// This is a pointer to distinguish between explicit zero and not specified.
	Replicas *int32 `yaml:"replicas,omitempty"`
}

// Specs for a specific component to create in AppStudio
type ComponentSpec struct {
	// The component name which will be created
	Name string `yaml:"name"`
	// The type indicate if the component comes from a private source like quay or github. Posible values: "private" or "public"
	Type string `yaml:"type"`
	// Indicate the container value
	ContainerSource string `yaml:"containerSource,omitempty"`
	// Indicate the devfile value
	Devfilesource string `yaml:"devfileSource,omitempty"`
	// Repository URL from where component will be created
	GitSourceUrl string `yaml:"gitSourceUrl,omitempty"`
	// An endpoint where the framework can ping to see if a component was deployed successfully
	HealthEndpoint string `yaml:"healthz"`
	// Set k8s resource specific properties
	K8sSpec K8sSpec `yaml:"spec,omitempty"`
}
