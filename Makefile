GOPATH = $(shell go env GOPATH)

.PHONY: generate test vendor

test:
	go test -timeout 30s -v ./...

tidy:
	go mod tidy

.PHONY: golangci-lint
golangci-lint: $(GOLANGCILINT)
	$(GOPATH)/bin/golangci-lint run --verbose

$(GOLANGCILINT):
	(cd /; GO111MODULE=on GOPROXY="direct" GOSUMDB=off go get github.com/golangci/golangci-lint/cmd/golangci-lint@v1.30.0)
