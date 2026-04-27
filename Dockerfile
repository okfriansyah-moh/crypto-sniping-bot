# ─── Stage 1: Build ────────────────────────────────────────────────────────────
# CGO is required by go-ethereum's c-kzg-4844 and secp256k1 C bindings.
# Debian bookworm matches the glibc in the distroless runtime — do not use alpine here.
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Cache module downloads as a separate layer — only re-runs when go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source and build the main binary.
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /build/crypto-sniping-bot \
    ./cmd/

# Build a minimal healthcheck binary.
# distroless has no shell or wget, so we need a native Go binary for HEALTHCHECK.
RUN printf 'package main\nimport("net/http";"os")\nfunc main(){r,e:=http.Get("http://localhost:8080/health");if e!=nil||r.StatusCode!=200{os.Exit(1)}}' \
    > /tmp/healthcheck.go \
    && CGO_ENABLED=0 go build -o /build/healthcheck /tmp/healthcheck.go

# ─── Stage 2: Runtime (distroless) ─────────────────────────────────────────────
# gcr.io/distroless/base-debian12 includes glibc, ca-certificates, and tzdata.
# :nonroot pins the image to run as uid 65532 (no shell, no package manager).
FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

# Application binary.
COPY --from=builder /build/crypto-sniping-bot /app/crypto-sniping-bot

# Healthcheck binary (no shell available in distroless).
COPY --from=builder /build/healthcheck /app/healthcheck

# YAML configuration files (parameters only — no secrets, those come via env vars).
COPY config/ /app/config/

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app/healthcheck"]

ENTRYPOINT ["/app/crypto-sniping-bot"]
CMD ["serve"]
