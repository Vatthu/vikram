# ============================================================
# Stage 1: Build the levik binary
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

RUN addgroup -S levik && adduser -S -G levik levik

ENV LEVIK_HOME=/home/levik/.levik

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -fsS http://localhost:18790/health >/dev/null || exit 1

# Copy binary
COPY --from=builder /src/build/levik /usr/local/bin/levik

RUN mkdir -p /home/levik && chown -R levik:levik /home/levik

USER levik

# Create levik home directory
RUN /usr/local/bin/levik onboard

ENTRYPOINT ["levik"]
CMD ["gateway"]
