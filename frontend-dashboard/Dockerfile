# frontend-dashboard — multi-stage build: Vite → nginx on port 3000.
FROM node:22-alpine AS build

WORKDIR /app

COPY frontend-dashboard/package.json frontend-dashboard/package-lock.json ./
RUN npm ci

COPY frontend-dashboard/ .

# Relative /api URLs; nginx proxies to dashboard-api. Key injected at build time from compose.
ARG VITE_DASHBOARD_API_KEY=
ARG VITE_API_BASE_URL=
ENV VITE_DASHBOARD_API_KEY=$VITE_DASHBOARD_API_KEY
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL

RUN npm run build

FROM nginx:1.27-alpine

COPY frontend-dashboard/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 3000

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:3000/ || exit 1
