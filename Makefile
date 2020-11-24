.PHONY: format test run build-image run-container

format:
	@gofmt -w *.go

test:
	@go test

run:
	@go run jlink.go util.go maven_central.go adoptium.go

build-image:
	@docker build -t 'jlink.online:latest' .

run-container: build-image
	@docker run --rm -p 80:80 jlink.online:latest
