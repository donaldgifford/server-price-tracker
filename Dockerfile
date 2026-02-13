FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server-price-tracker ./cmd/server-price-tracker

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /server-price-tracker /usr/local/bin/server-price-tracker
EXPOSE 8080
ENTRYPOINT ["server-price-tracker"]
CMD ["serve"]
