.PHONY: dev build test clean

dev:
	go run .

build:
	@mkdir -p bin
	go build -o bin/sdcard-dump .

test:
	go test ./...

clean:
	rm -rf bin
