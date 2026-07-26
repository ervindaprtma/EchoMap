# ---- build: compile the SPA ----
FROM node:20-alpine AS build
WORKDIR /app
# Copy manifests first so `npm ci` is cached until dependencies actually change.
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build          # tsc -b && vite build -> /app/dist

# ---- runtime: nginx serving static + reverse-proxying /api and /ws ----
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
