.PHONY: build run clean test

build: 
	go build -o checkmate main.go

run:
	go run main.go

clean:
	rm -f checkmate

test:
	go test ./...