.PHONY: setup run web test build check probe

GO_CMD := ./scripts/dev-go.sh

setup:
	./scripts/setup-dev.sh

run:
	./scripts/dev-daemon.sh

web:
	./scripts/dev-web.sh

test:
	$(GO_CMD) test ./...

build:
	$(GO_CMD) build -o camstationd ./cmd/camstationd

check:
	./scripts/check-dev.sh

probe:
	test -n "$$CAMSTATION_CAMERA_URL"
	$(GO_CMD) run ./cmd/camstationd -probe-only -camera-url "$$CAMSTATION_CAMERA_URL"
