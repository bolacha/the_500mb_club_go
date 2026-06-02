FROM golang:1.26.3-alpine AS builder

WORKDIR /build
COPY go.mod go.sum* ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -trimpath -ldflags="-s -w" -o /app ./cmd/api/

FROM scratch
COPY --from=builder /app /app
USER 10001:10001
EXPOSE 8000
ENTRYPOINT ["/app"]
