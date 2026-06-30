.PHONY: run test build probe

run:
	go run ./cmd/camstationd

test:
	go test ./...

build:
	go build -o camstationd ./cmd/camstationd

probe:
	test -n "$$CAMSTATION_CAMERA_URL"
	go run ./cmd/camstationd -probe-only -camera-url "$$CAMSTATION_CAMERA_URL"

