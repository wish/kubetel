GIT := $(shell git rev-parse --short HEAD)
UNAME_S := $(shell uname -s | tr A-Z a-z)



build/kubetel:
	$(shell ln -s kubetel.${UNAME_S} build/kubetel)


.PHONY: deps
deps: vendor
vendor: Gopkg.toml Gopkg.lock
	dep ensure

.PHONY: clean
clean: packr-clean
	rm -rf build
	rm -rf vendor
