FROM golang:1.27-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mock-server ./tools/mock-server

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
COPY --from=builder /mock-server /usr/local/bin/mock-server
COPY tools/mock-server/testdata /testdata
EXPOSE 8089
ENTRYPOINT ["mock-server"]
CMD ["--port", "8089", "--fixture", "/testdata/search_response.json"]
