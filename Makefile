.PHONY: build test
build:
	go build -o bin/collab ./cmd/collab
test: build
	go test -race ./...
	python3 -m unittest discover -s scripts -p 'test_*.py'
