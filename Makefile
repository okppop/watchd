.PHONY: build test clean

build:
	npm --prefix ui ci
	npm --prefix ui run build
	go build -o bin/watchd ./cmd

test:
	npm --prefix ui run build
	go test -race ./...

clean:
	rm -rf bin ui/dist
