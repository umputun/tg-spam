GO_FILES=$(shell find . -name '*.go' | grep -vE 'vendor')

lint-fix:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.48.0
	go install golang.org/x/tools/cmd/goimports@latest
	goimports -w $(GO_FILES)
	go fmt ./...
	golangci-lint -v run ./...

test:
	go test -count 1 -v -race ./...

test_multi:
	go test -count 100 -v -race ./...

bench:
	go test -bench=. -benchmem -v -run Benchmark ./...

coverage:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...