# Multi-stage build for Bounty Agent
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /bounty ./cmd/bounty/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates sqlite-libs bash ripgrep git curl \
    && addgroup -S bounty && adduser -S -G bounty bounty

COPY --from=builder /bounty /usr/local/bin/bounty

RUN mkdir -p /home/bounty/.local/share/bounty /workspace \
    && chown -R bounty:bounty /home/bounty /workspace

VOLUME ["/workspace"]
WORKDIR /workspace

USER bounty

ENV BOUNTY_HOME=/home/bounty/.local/share/bounty

ENTRYPOINT ["bounty"]
CMD ["serve"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1