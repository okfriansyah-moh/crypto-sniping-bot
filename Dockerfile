# ─── Stage 1: Build ────────────────────────────────────────────────────────────
# golang:1.25-alpine has a minimal CVE surface (alpine musl toolchain).
# We build a fully static binary with -linkmode external -extldflags '-static'
# so the runtime image needs zero shared libraries — enabling the use of
# gcr.io/distroless/static-debian12 which contains NO OS packages and zero CVEs.
FROM golang:1.25-alpine AS builder

# musl-dev + gcc are the only additions needed for CGO static linking on alpine.
RUN apk add --no-cache gcc musl-dev

WORKDIR /build

# Cache module downloads as a separate layer — only re-runs when go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source and build a fully static binary.
# -linkmode external -extldflags '-static' produces a self-contained binary with
# no runtime dependency on glibc, musl, libgcc, or any other shared library.
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -linkmode external -extldflags '-static'" \
    -o /build/crypto-sniping-bot \
    ./cmd/

# Build a minimal static healthcheck binary.
# distroless/static has no shell or wget — a native Go binary is required.
# CGO_ENABLED=0 produces a pure-Go static binary with no C dependencies.
RUN printf 'package main\nimport("net/http";"os")\nfunc main(){r,e:=http.Get("http://localhost:8080/health");if e!=nil||r.StatusCode!=200{os.Exit(1)}}' \
    > /tmp/healthcheck.go \
    && CGO_ENABLED=0 go build -o /build/healthcheck /tmp/healthcheck.go

# ─── Stage 2: Runtime (distroless/static) ──────────────────────────────────────
# gcr.io/distroless/static-debian12:nonroot contains ONLY:
#   - CA certificates
#   - Timezone data
#   - /etc/passwd (for uid 65532 nonroot)
# No glibc, no libgcc, no OS packages — scanner reports 0 CVEs.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Application binary (statically linked — no shared library deps).
COPY --from=builder /build/crypto-sniping-bot /app/crypto-sniping-bot

# Healthcheck binary (pure-Go static binary).
COPY --from=builder /build/healthcheck /app/healthcheck

# YAML configuration files (parameters only — no secrets, those come via env vars).
COPY config/ /app/config/

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app/healthcheck"]

ENTRYPOINT ["/app/crypto-sniping-bot"]
CMD ["serve"]
