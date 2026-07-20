.PHONY: build test vet lint clean install

BINARY := bounty
CMD_DIR := ./cmd/bounty

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) $(CMD_DIR)

build-cgo:
	CGO_ENABLED=1 go build -ldflags="-s -w" -o $(BINARY) $(CMD_DIR)

test:
	go test ./... -count=1 -timeout=60s

vet:
	go vet ./...

fmt:
	gofmt -w .

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)

install: build
	cp $(BINARY) /usr/local/bin/$(BINARY)

run: build
	./$(BINARY) chat

doctor: build
	./$(BINARY) doctor

docker-build:
	docker build -t bounty-agent .

docker-run:
	docker run -it --rm -v $$(pwd):/workspace -e DEEPSEEK_API_KEY bounty-agent chat
