FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.sum go.sum
COPY go.mod go.mod
RUN go mod download

COPY main.go main.go
COPY pkg pkg

RUN go build -o story-api

FROM alpine:3.23.3
WORKDIR /app

COPY --from=builder /src/story-api /app/story-api

CMD ["/app/story-api"]
