DIRECTORY := $(PWD)
CLGENIMAGE := quay.io/slok/kube-code-generator:v1.16.7
PROJECT_PACKAGE := "github.com/mkimuram/k8s-ext-connector"
GROUPS_VERSION := "submariner:v1alpha1"
FORWARDER_IMAGE := forwarder
FORWARDER_VERSION := v0.3.0
GATEWAY_IMAGE := gateway
GATEWAY_VERSION := v0.3.0
OPERATOR_IMAGE := k8s-ext-connector
OPERATOR_VERSION := v0.3.0
IMAGE_REPO := docker.io/mkimuram

.PHONY: build-all push-all forwarder forwarder-image push-forwarder gateway gateway-image push-gateway operator push-operator clean generate-client

build-all: forwarder-image gateway-image operator

push-all: push-forwarder push-gateway push-operator

forwarder:
	cd forwarder && mkdir -p bin && GO111MODULE=on go build -o bin/forwarder .

forwarder-image: forwarder
	cd forwarder && docker build -t $(FORWARDER_IMAGE):$(FORWARDER_VERSION) .

push-forwarder:
	docker tag $(FORWARDER_IMAGE):$(FORWARDER_VERSION) $(IMAGE_REPO)/$(FORWARDER_IMAGE):$(FORWARDER_VERSION)
	docker push $(IMAGE_REPO)/$(FORWARDER_IMAGE):$(FORWARDER_VERSION)

gateway:
	cd gateway && mkdir -p bin && GO111MODULE=on go build -o bin/gateway .

gateway-image: gateway
	cd gateway && docker build -t $(GATEWAY_IMAGE):$(GATEWAY_VERSION) .

push-gateway:
	docker tag $(GATEWAY_IMAGE):$(GATEWAY_VERSION) $(IMAGE_REPO)/$(GATEWAY_IMAGE):$(GATEWAY_VERSION)
	docker push $(IMAGE_REPO)/$(GATEWAY_IMAGE):$(GATEWAY_VERSION)

operator:
	operator-sdk build $(OPERATOR_IMAGE):$(OPERATOR_VERSION)

push-operator:
	docker tag $(OPERATOR_IMAGE):$(OPERATOR_VERSION) $(IMAGE_REPO)/$(OPERATOR_IMAGE):$(OPERATOR_VERSION)
	docker push $(IMAGE_REPO)/$(OPERATOR_IMAGE):$(OPERATOR_VERSION)

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

.PHONY: test-all test-lint test-vet test-unit test-e2e
test-all: test-lint test-vet test-unit test-e2e

test-lint:
	golint `go list ./... | grep -v -e 'pkg/apis' -e 'pkg/client' -e 'mock_'`

test-vet:
	go vet ./...

test-unit:
	go test `go list ./... | grep -v -e 'pkg/apis' -e 'pkg/client' -e 'mock_' -e 'test/e2e'`

test-e2e:
	operator-sdk test local ./test/e2e --namespace=external-services --debug --go-test-flags="-v -ginkgo.v"

.PHONY: release

release: clean test-all build-all push-all
