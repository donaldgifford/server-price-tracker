FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mock-server ./tools/mock-server

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
COPY --from=builder /mock-server /usr/local/bin/mock-server
COPY tools/mock-server/testdata /testdata
EXPOSE 8089
ENTRYPOINT ["mock-server"]
CMD ["--port", "8089", "--fixture", "/testdata/search_response.json"]
