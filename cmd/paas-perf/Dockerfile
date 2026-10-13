FROM golang:1.27.1-bookworm@sha256:8d48e12ec56735e9358640898b9d9b9fcca110612ed8a5567438c0a1baa24e66 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download github.com/coder/websocket
COPY cmd/paas-perf ./cmd/paas-perf
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /app ./cmd/paas-perf
FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0
COPY --from=build /app /app
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app", "serve"]
