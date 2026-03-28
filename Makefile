.PHONY: dev build test clean

dev: build
	./bin/sdcard-dump

build:
	@mkdir -p bin
	go build -o bin/sdcard-dump .

test:
	go test ./...

clean:
	rm -rf bin
