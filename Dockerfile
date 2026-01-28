ARG NODE_VERSION=lts-alpine
ARG NGINX_VERSION=1.27-alpine

# ========================================
# Base stage with dependencies
# ========================================
FROM node:${NODE_VERSION} AS base
WORKDIR /app

# Install pnpm globally
RUN npm install -g pnpm@latest

# Copy package files for dependency installation
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml* ./
COPY nx.json tsconfig.base.json ./

# Copy all project.json files to enable Nx dependency graph
COPY apps/api/project.json apps/api/
COPY apps/web/project.json apps/web/
COPY libs/api/ libs/api/
COPY libs/web/ libs/web/

# Install all dependencies
RUN pnpm install --frozen-lockfile

# ========================================
# Build stage
# ========================================
FROM base AS builder
WORKDIR /app

# Copy source code
COPY apps ./apps
COPY libs ./libs
COPY tsconfig.base.json nx.json ./

# Build both applications
RUN pnpm nx run api:build:production --skip-nx-cache
RUN pnpm nx run web:build:production --skip-nx-cache

# Prune dev dependencies for production
RUN pnpm install --prod --frozen-lockfile --ignore-scripts

# ========================================
# API production image
# ========================================
FROM node:${NODE_VERSION} AS api
WORKDIR /app

# Create non-root user
RUN addgroup -g 1001 -S nodejs && \
    adduser -S nodejs -u 1001

# Copy production dependencies and built app
COPY --from=builder --chown=nodejs:nodejs /app/node_modules ./node_modules
COPY --from=builder --chown=nodejs:nodejs /app/dist/apps/api ./dist

# Switch to non-root user
USER nodejs

# Expose API port
EXPOSE 3333

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD node -e "require('http').get('http://localhost:3333/api/health', (r) => {process.exit(r.statusCode === 200 ? 0 : 1)})"

# Start the API
CMD ["node", "dist/main.js"]

# ========================================
# Web production image
# ========================================
FROM nginx:${NGINX_VERSION} AS web

# Copy custom nginx config
COPY nginx.conf /etc/nginx/nginx.conf

# Copy built web application
COPY --from=builder /app/dist/apps/web/browser /usr/share/nginx/html

# Copy config file if it exists
COPY --chown=nginx:nginx .config.json* /usr/share/nginx/html/

# Create non-root nginx user and set permissions
RUN chown -R nginx:nginx /usr/share/nginx/html && \
    chown -R nginx:nginx /var/cache/nginx && \
    chown -R nginx:nginx /var/log/nginx && \
    touch /var/run/nginx.pid && \
    chown -R nginx:nginx /var/run/nginx.pid

# Switch to non-root user
USER nginx

# Expose ports
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

# Start nginx
CMD ["nginx", "-g", "daemon off;"]
