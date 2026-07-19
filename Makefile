.PHONY: build run clean tidy lint linux-release

BINARY=scoreboard

build:
	CGO_ENABLED=1 go build -o $(BINARY) .

run:
	go run .

linux-release:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY)-linux-amd64 .
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o $(BINARY)-linux-arm64 .

tidy:
	go mod tidy

lint:
	go vet ./...

clean:
	rm -f $(BINARY) $(BINARY)-linux-*
