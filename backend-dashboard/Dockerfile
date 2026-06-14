# backend-dashboard — read-only operator API (distroless static binary, port 8090).
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -linkmode external -extldflags '-static'" \
    -o /build/dashboard-api \
    ./backend-dashboard/cmd/...

RUN mkdir -p /build/output/logs

RUN printf 'package main\nimport("net/http";"os")\nfunc main(){r,e:=http.Get("http://localhost:8090/api/v1/health");if e!=nil||r.StatusCode!=200{os.Exit(1)}}' \
    > /tmp/healthcheck.go \
    && CGO_ENABLED=0 go build -o /build/healthcheck /tmp/healthcheck.go

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /build/dashboard-api /app/dashboard-api
COPY --from=builder /build/healthcheck /app/healthcheck
COPY shared/config/ /app/config/
COPY --from=builder /build/output/logs /app/output/logs

EXPOSE 8090

HEALTHCHECK --interval=15s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/app/healthcheck"]

ENTRYPOINT ["/app/dashboard-api"]
CMD ["serve"]
