.PHONY: dev build test clean

dev:
	go run .

build:
	go build -o sdcard-dump .

test:
	go test ./...

clean:
	rm -f sdcard-dump
