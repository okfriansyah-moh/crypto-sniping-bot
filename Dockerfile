# ─── Stage 1: Build ────────────────────────────────────────────────────────────
# CGO is required by go-ethereum's c-kzg-4844 and secp256k1 C bindings.
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /build

# Cache module downloads as a separate layer — only re-runs when go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source and build.
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /build/crypto-sniping-bot \
    ./cmd/

# ─── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: required for outbound HTTPS RPC calls
# tzdata: required for timezone-aware structured logging
# libgcc: required by CGO-linked binaries (go-ethereum secp256k1 / c-kzg-4844)
RUN apk add --no-cache ca-certificates tzdata libgcc wget

# Run as a non-root user — principle of least privilege.
RUN addgroup -S sniper && adduser -S sniper -G sniper

WORKDIR /app

# Application binary.
COPY --from=builder --chown=sniper:sniper /build/crypto-sniping-bot /app/crypto-sniping-bot

# YAML configuration files (parameters only — no secrets, those come via env vars).
COPY --chown=sniper:sniper config/ /app/config/

# Keypair mount point for Solana wallets.
# Bind-mounted at runtime via docker-compose; not baked into the image.
RUN mkdir -p /keys && chown sniper:sniper /keys

USER sniper

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/crypto-sniping-bot"]
CMD ["serve"]
