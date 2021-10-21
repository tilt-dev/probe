GOPATH = $(shell go env GOPATH)

.PHONY: generate test vendor

test:
ifneq ($(CIRCLECI),true)
		go test -timeout 30s -v ./...
else
		mkdir -p test-results
		gotestsum --format standard-quiet --junitfile test-results/unit-tests.xml -- ./... -timeout 30s
endif

tidy:
	go mod tidy

fmt:
	goimports -w -l -local github.com/tilt-dev/probe pkg/

.PHONY: golangci-lint
golangci-lint: $(GOLANGCILINT)
	$(GOPATH)/bin/golangci-lint run --verbose

$(GOLANGCILINT):
	(cd /; GO111MODULE=on GOPROXY="direct" GOSUMDB=off go get github.com/golangci/golangci-lint/cmd/golangci-lint@v1.30.0)
