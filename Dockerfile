# Build Stage
FROM golang:latest AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

RUN go build \
    -trimpath \
    -ldflags="-s -w" \
    -o mirror \
    main.go

# Runtime Stage
FROM gcr.io/distroless/static-debian12

WORKDIR /app

COPY --from=builder /app/mirror .
COPY --from=builder /app/static ./static

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/mirror"]