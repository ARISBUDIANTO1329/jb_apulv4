FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates ffmpeg

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

FROM alpine:3.19

RUN apk add --no-cache ca-certificates ffmpeg bash

WORKDIR /app

COPY --from=builder /app/server .
COPY --from=builder /app/internal/templates ./internal/templates
COPY --from=builder /app/static ./static

EXPOSE 8000

CMD ["./server"]
