# --platform=$BUILDPLATFORM with TARGETOS/TARGETARCH makes the image native
# on amd64 and arm64 hosts instead of always emulating amd64.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=docker
ARG COMMIT=none

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags="-w -s -X main.Version=$VERSION -X main.Commit=$COMMIT -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o odata-mcp ./cmd/odata-mcp

FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata \
    && adduser -D -u 1001 appuser

WORKDIR /app

COPY --from=builder /build/odata-mcp /usr/local/bin/odata-mcp
COPY hints.json /app/hints.json

USER appuser

EXPOSE 8080

ENTRYPOINT ["odata-mcp"]
CMD ["--help"]
