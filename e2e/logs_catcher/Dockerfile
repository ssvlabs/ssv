FROM golang:latest

WORKDIR /

COPY . .

RUN go get -d -v ./...


RUN go build -o ./logs_catcher ./cmd/logs_catcher

ENTRYPOINT ["./logs_catcher"]