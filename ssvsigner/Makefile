.PHONY: docker-build
docker-build:
	cd .. && docker build -f ssvsigner/Dockerfile -t ssv-signer:latest .

.PHONY: test-e2e
test-e2e: docker-build
	cd e2e && go test -count=1 -v ./signing/ -timeout=600s