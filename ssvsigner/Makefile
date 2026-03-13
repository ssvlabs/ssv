.PHONY: docker-build
docker-build:
	cd .. && docker build -f ssvsigner/Dockerfile -t ssv-signer:latest .

.PHONY: test-e2e
test-e2e: docker-build test-e2e-no-build

.PHONY: test-e2e-no-build
test-e2e-no-build:
	cd e2e && go test -p 16 -parallel 256 -v ./signing/ -timeout 10m
