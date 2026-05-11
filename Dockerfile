FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
# Install templ CLI for generating *_templ.go files (IMPL-0015 Phase 4).
# These are gitignored; the binary will fail to compile without them.
# Version must match github.com/a-h/templ in go.mod — bump in lockstep
# with mise.toml and .goreleaser.yml.
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1001
COPY . .
RUN templ generate
RUN CGO_ENABLED=0 go build -o /server-price-tracker ./cmd/server-price-tracker

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /server-price-tracker /usr/local/bin/server-price-tracker
EXPOSE 8080
ENTRYPOINT ["server-price-tracker"]
