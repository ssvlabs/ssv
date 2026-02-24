# syntax=docker/dockerfile:1

FROM golang:1.24.10 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -o /out/mev-mock-relay ./cmd/mev-mock-relay
RUN CGO_ENABLED=0 go build -trimpath -o /out/mev-builder-smoke ./cmd/mev-builder-smoke
RUN CGO_ENABLED=0 go build -trimpath -o /out/mev-smoke-client ./cmd/mev-smoke-client

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/mev-mock-relay /app/mev-mock-relay
COPY --from=build /out/mev-builder-smoke /app/mev-builder-smoke
COPY --from=build /out/mev-smoke-client /app/mev-smoke-client

