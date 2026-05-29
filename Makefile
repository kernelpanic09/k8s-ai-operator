BINARY_NAME    := manager
IMAGE_REPO     := ghcr.io/kernelpanic09/k8s-ai-operator
IMAGE_TAG      ?= latest
CONTROLLER_GEN := go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.0
GOLANGCI_LINT  := go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.61.0

# Go build flags: static binary, no CGO, embed version info.
LDFLAGS := -w -s
GOFLAGS := CGO_ENABLED=0 GOOS=linux GOARCH=amd64

.PHONY: build
build: ## Build the manager binary.
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/manager

.PHONY: run
run: ## Run the operator locally against the cluster in your current kubeconfig.
	go run ./cmd/manager \
		--leader-elect=false \
		--log-level=debug \
		--proxy-namespace=ai-operator-system \
		--proxy-service-name=k8s-ai-operator-proxy

.PHONY: test
test: ## Run all tests with race detection and coverage.
	go test -race -cover ./...

.PHONY: test-unit
test-unit: ## Run unit tests only (no envtest required).
	go test -race -cover ./internal/bedrock/... ./internal/proxy/...

.PHONY: fmt
fmt: ## Run gofmt over all source files.
	gofmt -w -l .

.PHONY: vet
vet: ## Run go vet.
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint.
	$(GOLANGCI_LINT) run ./...

.PHONY: manifests
manifests: ## Regenerate CRD YAML from Go types. Requires controller-gen.
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=k8s-ai-operator paths="./internal/controller/..." output:rbac:artifacts:config=config/rbac
	$(CONTROLLER_GEN) webhook paths="./internal/webhook/..." output:webhook:artifacts:config=config/webhook

.PHONY: generate
generate: ## Regenerate deepcopy methods. Requires controller-gen.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: install
install: manifests ## Install CRDs into the cluster in your current kubeconfig.
	kubectl apply -k config/crd

.PHONY: uninstall
uninstall: ## Remove CRDs from the cluster.
	kubectl delete -k config/crd --ignore-not-found=true

.PHONY: deploy
deploy: manifests ## Deploy the operator to the cluster.
	kubectl apply -k config/default

.PHONY: undeploy
undeploy: ## Remove the operator from the cluster.
	kubectl delete -k config/default --ignore-not-found=true

.PHONY: docker-build
docker-build: ## Build the operator Docker image.
	docker build -t $(IMAGE_REPO):$(IMAGE_TAG) .

.PHONY: docker-push
docker-push: ## Push the Docker image.
	docker push $(IMAGE_REPO):$(IMAGE_TAG)

.PHONY: docker-build-push
docker-build-push: docker-build docker-push ## Build and push in one step.

.PHONY: samples
samples: ## Apply sample CRs to the cluster (for local dev/demo).
	kubectl apply -f config/samples/pii_guardrail.yaml
	kubectl apply -f config/samples/claude_haiku_endpoint.yaml
	kubectl apply -f config/samples/code_review_prompt.yaml

.PHONY: help
help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
