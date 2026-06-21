# ============================================================
# Stage 1: Build the vikram binary
# ============================================================
FROM golang:1.26.0-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN make build

# ============================================================
# Stage 2: Minimal runtime image
# ============================================================
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata curl

RUN addgroup -S vikram && adduser -S -G vikram vikram

ENV VIKRAM_HOME=/home/vikram/.vikram

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -fsS http://localhost:18790/health >/dev/null || exit 1

# Copy binary
COPY --from=builder /src/build/vikram /usr/local/bin/vikram

RUN mkdir -p /home/vikram && chown -R vikram:vikram /home/vikram

USER vikram

# Create vikram home directory
RUN /usr/local/bin/vikram onboard

ENTRYPOINT ["vikram"]
CMD ["gateway"]
