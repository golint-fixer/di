export GO15VENDOREXPERIMENT=1
PACKAGES=$(shell GO15VENDOREXPERIMENT=1 go list ./... | grep -v vendor)
NOVENDOR=$(shell find . -path ./vendor -prune -o -name '*.go' -print)
REPO = quay.io/netsys
DOCKER = docker

all:
	cd -P . && \
	go build . && \
	go build -o ./minion/minion ./minion && \
	go build -o ./inspect/inspect ./inspect

install:
	cd -P . && go install . && \
	go install ./inspect

generate:
	go generate $(PACKAGES)

providers:
	python3 scripts/gce-descriptions > provider/gceConstants.go

format:
	gofmt -w -s $(NOVENDOR)

check:
	go test $(PACKAGES)

lint: format
	cd -P . && go vet $(PACKAGES)
	for package in `echo $(PACKAGES) | grep -v minion/pb`; do \
		golint -min_confidence .25 $$package ; \
	done

coverage: db.cov dsl.cov engine.cov cluster.cov join.cov minion/supervisor.cov minion/network.cov minion.cov provider.cov

%.cov:
	go test -coverprofile=$@.out ./$*
	go tool cover -html=$@.out -o $@.html
	rm $@.out

# BUILD

docker-build-di:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .
	${DOCKER} build -t ${REPO}/di .

docker-build-tester:
	cd -P di-tester && ${DOCKER} build -t ${REPO}/di-tester .

docker-build-minion:
	cd -P minion && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build . \
	 && ${DOCKER} build -t ${REPO}/di-minion .

# PUSH

docker-push-di:
	${DOCKER} push ${REPO}/di

docker-push-tester:
	${DOCKER} push ${REPO}/di-tester

docker-push-minion:
	${DOCKER} push ${REPO}/di-minion

# Include all .mk files so you can have your own local configurations
include $(wildcard *.mk)
