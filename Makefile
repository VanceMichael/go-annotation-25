GOTOOLCHAIN ?= local
export GOTOOLCHAIN

.PHONY: build test race vet fmt selfcheck docker clean

build:
	go build -o bin/oilctl ./cmd/oilctl

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

selfcheck: build
	./bin/oilctl selfcheck

docker:
	docker build -t wasteoil:local .

clean:
	rm -rf bin
