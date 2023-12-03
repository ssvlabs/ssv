FROM golang:latest

WORKDIR /app/

COPY . .

RUN go get -d -v ./...
RUN #go install -v ./...
RUN CGO_ENABLED=1 go build -o beacon_proxy ./cmd/beacon_proxy
RUN chmod +x ./beacon_proxy
#RUN --network=host # Needed?


ENTRYPOINT ["/app/beacon_proxy"]

