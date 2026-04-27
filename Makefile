GO ?= go
BIN ?= app
OUTDIR ?= bin
WEB_DIR ?= web
CGO_ENABLED ?= 0

.PHONY: build build-web dev test vet web-lint web-build

build: build-web
	@mkdir -p $(OUTDIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -tags embed -o $(OUTDIR)/$(BIN) ./cmd/willing

build-web:
	cd $(WEB_DIR) && npm install && npm run build

dev:
	$(GO) run ./cmd/willing serve

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

web-lint:
	cd $(WEB_DIR) && npm run lint

web-build:
	cd $(WEB_DIR) && npm run build
