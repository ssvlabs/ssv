FROM golang:latest

WORKDIR /go/src/app

COPY . .

RUN go get -d -v ./...
RUN go install -v ./...


RUN --network=host # Needed?

CMD ["app"]