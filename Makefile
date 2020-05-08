DIRECTORY := $(PWD)
CLGENIMAGE := quay.io/slok/kube-code-generator:v1.16.7
PROJECT_PACKAGE := "github.com/mkimuram/k8s-ext-connector"
GROUPS_VERSION := "submariner:v1alpha1"

.PHONY: all forwarder forwarder-image gateway operator clean generate-client

all: forwarder-image gateway operator

forwarder:
	cd forwarder && mkdir -p bin && GO111MODULE=on go build -o bin/forwarder .

forwarder-image: forwarder
	cd forwarder && docker build -t forwarder:0.2 .

gateway:
	cd gateway && mkdir -p bin && GO111MODULE=on go build -o bin/gateway .

operator:
	operator-sdk build k8s-ext-connector:v0.0.1

clean:
	rm -f forwarder/bin/forwarder gateway/bin/gateway

generate-client:
	docker run -it --rm \
    -v $(DIRECTORY):/go/src/$(PROJECT_PACKAGE) \
    -e PROJECT_PACKAGE=$(PROJECT_PACKAGE) \
    -e CLIENT_GENERATOR_OUT=$(PROJECT_PACKAGE)/pkg/client \
    -e APIS_ROOT=$(PROJECT_PACKAGE)/pkg/apis \
    -e GROUPS_VERSION=$(GROUPS_VERSION) \
    -e GENERATION_TARGETS="client,lister,informer" \
    $(CLGENIMAGE)

.PHONY: test-all test-lint test-unit test-e2e
test-all: test-lint test-unit test-e2e

test-lint:
	golint `go list ./... | grep -v -e 'pkg/apis' -e 'pkg/client' -e 'mock_'`

test-unit:
	go test `go list ./... | grep -v -e 'pkg/apis' -e 'pkg/client' -e 'mock_' -e 'test/e2e'`

test-e2e:
	operator-sdk test local ./test/e2e --namespace=external-services --debug --go-test-flags="-v -ginkgo.v"

.PHONY: release

release: clean test-all all
