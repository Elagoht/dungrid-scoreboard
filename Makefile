.PHONY: build run clean tidy lint linux-release

BINARY=scoreboard

build:
	go build -o $(BINARY) .

run:
	go run .

linux-release:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o $(BINARY)-linux-arm64 .

tidy:
	go mod tidy

lint:
	go vet ./...

clean:
	rm -f $(BINARY) $(BINARY)-linux-*
