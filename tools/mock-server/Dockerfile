FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mock-server ./tools/mock-server

FROM alpine:3.21
COPY --from=builder /mock-server /usr/local/bin/mock-server
COPY tools/mock-server/testdata /testdata
EXPOSE 8089
ENTRYPOINT ["mock-server"]
CMD ["--port", "8089", "--fixture", "/testdata/search_response.json"]
