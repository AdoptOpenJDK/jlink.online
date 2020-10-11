format:
	@gofmt -w *.go

test:
	@go test

run:
	@go run jlink.go util.go maven_central.go adoptium.go

build:
	@docker build -t 'jlink.online:latest' .

push: build docker-login
	docker tag jlink.online $(DOCKER_REPO):latest
	docker tag jlink.online $(DOCKER_REPO):$(VERSION)
	docker push $(DOCKER_REPO):latest
	docker push $(DOCKER_REPO):$(VERSION)

docker-login:
	@eval "eval $$\( aws ecr --region us-east-2 get-login --no-include-email \)"

run-container: build
	@docker run --rm -p 80:80 --tmpfs /tmp jlink.online:latest