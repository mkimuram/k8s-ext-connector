IMAGE := quay.io/slok/kube-code-generator:v1.16.7

DIRECTORY := $(PWD)
PROJECT_PACKAGE := "github.com/mkimuram/k8s-ext-connector"
GROUPS_VERSION := "submariner:v1alpha1"

.PHONY: generate-client
generate-client:
	docker run -it --rm \
    -v $(DIRECTORY):/go/src/$(PROJECT_PACKAGE) \
    -e PROJECT_PACKAGE=$(PROJECT_PACKAGE) \
    -e CLIENT_GENERATOR_OUT=$(PROJECT_PACKAGE)/pkg/client \
    -e APIS_ROOT=$(PROJECT_PACKAGE)/pkg/apis \
    -e GROUPS_VERSION=$(GROUPS_VERSION) \
    -e GENERATION_TARGETS="client" \
    $(IMAGE)
