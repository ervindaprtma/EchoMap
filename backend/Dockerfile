# ---- build ----
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/echomap ./cmd/echomap

# ---- runtime ----
FROM alpine:3.20
# setcap so the NON-ROOT process can open raw ICMP sockets with the container's
# NET_RAW capability (cap_add alone doesn't reach a non-root user's effective set).
RUN apk add --no-cache libcap ca-certificates
COPY --from=build /out/echomap /app/echomap
RUN setcap cap_net_raw+ep /app/echomap && adduser -D -u 10001 echomap
USER echomap
ENTRYPOINT ["/app/echomap"]
CMD ["--role=api"]
