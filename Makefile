.PHONY: dev build test clean

dev: build
	./bin/dump

build:
	@mkdir -p bin
	go build -o bin/dump .

test:
	go test ./...

clean:
	rm -rf bin
