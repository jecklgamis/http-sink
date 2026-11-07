IMAGE_NAME:=http-sink
IMAGE_TAG:=main

default:
	@echo "Makefile targets:"
	@echo "make build - build the http-sink binary"
	@echo "make up - build and run Docker image"
	@echo "make all - build Docker image"
	@echo "make run-app-dev-mode - run app directly with the race detector and error details enabled"
	@echo "See Makefile for other useful targets"

all: tests int-tests dist image chart
up: all run
dist:
	@./generate-ssl-certs.sh
	@./generate-build-info.sh
image:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .
run:
	docker run -p 38080:38080 -p 8443:8443 -v http-sink-data:/data $(IMAGE_NAME):$(IMAGE_TAG)
build:
	go build -o http-sink .
run-app-dev-mode:
	APP_ENV=development go run -race .
run-bash:
	docker run -i -t --entrypoint /bin/sh $(IMAGE_NAME):$(IMAGE_TAG)
exec-bash:
	docker exec -it `docker ps | grep $(IMAGE_NAME) | awk '{print $$1}'` /bin/sh
tests:
	go vet ./...
	go test -race ./internal/...
int-tests:
	go test -race -count=1 ./test/integration/...
clean:
	@rm -f server.crt server.key http-sink
upgrade-deps:
	go get -u ./...
	go mod tidy
chart:
	cd deployment/k8s/helm && make package

