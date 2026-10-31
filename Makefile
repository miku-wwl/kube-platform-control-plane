.PHONY: api web web-build stage2-check

api:
	go run ./cmd/platform-api

web:
	npm --prefix web run dev

web-build:
	npm --prefix web run build

stage2-check:
	go test ./...
	go vet ./...
	npm --prefix web run build
