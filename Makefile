.PHONY: build test check clean
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/ami ./cmd/ami
test:
	go test -race -timeout 3m ./...
check: test
	go vet ./...
clean:
	rm -rf dist
