.PHONY: build clean test

build:
	go build -o bin/mira-review-parser ./cmd/mira-review-parser
	go build -o bin/mira-review-reply ./cmd/mira-review-reply

clean:
	rm -rf bin/

test:
	go test ./...
