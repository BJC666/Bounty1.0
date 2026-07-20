# Multi-stage build for Bounty Agent
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /bounty ./cmd/bounty/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates sqlite-libs bash ripgrep git curl

COPY --from=builder /bounty /usr/local/bin/bounty

RUN mkdir -p /root/.local/share/bounty

VOLUME ["/workspace"]
WORKDIR /workspace

ENV BOUNTY_HOME=/root/.local/share/bounty

ENTRYPOINT ["bounty"]
CMD ["chat"]
