FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

ENV CGO_ENABLED=1

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o clearnode

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/clearnode .

EXPOSE 8000 4242

CMD ["./clearnode"]
