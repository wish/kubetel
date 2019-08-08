PKGS 		:= $(shell go list ./... | grep -v gok8s)
GOFILES_BUILD   := $(shell find . -type f -iname "*.go" | grep -v '^.test\/')

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {sub("\\\\n",sprintf("\n%22c"," "), $$2);printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: clean
clean: ## Removes all build artifacts
	@rm -f kubetel

.PHONY: lint
lint: ## Runs linter
	@golint -set_exit_status ${PKGS}

.PHONY: vet
vet: ## Runs go vet
	@go vet ${PKGS}

.PHONY: test
test: ## Runs test
	@go test ${PKGS} -cover -race -count=1

kubetel: $(GOFILES_BUILD)
	go build .

.PHONY: build
build: kubetel ## Builds kubetel

