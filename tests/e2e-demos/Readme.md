# E2E DEMOS

The e2e-demos suite contains a set of tests that covers AppStudio demos.

Steps to run 'e2e-demos-suite':

1) Follow the instructions from the [Readme](../../docs/Installation.md) scripts to install AppStudio in e2e mode
2) Run the e2e suite: `./bin/e2e-appstudio --ginkgo.focus="e2e-demos-suite"`

## Test Generator

The test specs in e2e-demo-suite are generated dynamically using ginkgo specs. To run the demo you need to use the default
yaml located in `$ROOT_DIR/tests/e2e-demos/config/default.yaml`.

Also it is possible to create your own yaml following the next structure:

```yaml
tests: 
  - name: "create an application with nodejs component"
    applicationName: "e2e-nodejs"
    components:
      - name: "nodejs-component"
        type: "public"
        gitSourceUrl: "https://github.com/jduimovich/single-nodejs-app"
        devfileSource: "https://raw.githubusercontent.com/jduimovich/appstudio-e2e-demos/main/demos/single-nodejs-app/devfiles/devfile.yaml"
        language: "nodejs"
        healthz: "/"
        spec:
          replicas: 2
```

To run the e2e-demos with a custom yaml use: `./bin/e2e-appstudio --ginkgo.focus="e2e-demos-suite" -config-suites=$PATH_TO_YOUR_CONFIG_YAML`
