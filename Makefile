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

.PHONY: eval-selfcheck eval-smoke eval-run eval-judge eval-report
EVAL := scripts/eval
MODELS ?= qwen/qwen3.8-max
TASKS ?= A1,A2,A3,A4,A5,A6,A7,A8,A9,A10,B1,B2,B3,B4,B5,B6,B7,B8,B9,B10,C1,C2,C3,C4,C5,C6,C7,C8,C9,C10

eval-selfcheck:
	python $(EVAL)/selfcheck.py

eval-run:
	python $(EVAL)/runner.py --models $(MODELS) --task-ids $(TASKS)

eval-smoke:
	python $(EVAL)/runner.py --models $(MODELS) --task-ids A1,A2,A3,A4,A5,B1,B2,B3,C1,C2 --run-id smoke --redo
	python $(EVAL)/judge.py --run $(EVAL)/work/smoke

eval-judge:
	python $(EVAL)/judge.py --run $(EVAL)/work/$(RUN)

eval-report:
	python $(EVAL)/report.py --run $(EVAL)/work/$(RUNS)
