FROM node:22-alpine AS builder

WORKDIR /app

ENV NEXT_TELEMETRY_DISABLED=1

COPY package.json package-lock.json ./
RUN npm ci

COPY . .

RUN npm run build \
  && npm prune --omit=dev \
  && npm cache clean --force

FROM node:22-alpine AS runner

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV SPARKLE_API_BASE=http://host.docker.internal:1323/api

RUN addgroup -S nextjs \
  && adduser -S nextjs -G nextjs

COPY --from=builder --chown=nextjs:nextjs /app/package.json ./package.json
COPY --from=builder --chown=nextjs:nextjs /app/package-lock.json ./package-lock.json
COPY --from=builder --chown=nextjs:nextjs /app/next.config.mjs ./next.config.mjs
COPY --from=builder --chown=nextjs:nextjs /app/node_modules ./node_modules
COPY --from=builder --chown=nextjs:nextjs /app/.next ./.next
COPY --from=builder --chown=nextjs:nextjs /app/public ./public

USER nextjs

EXPOSE 3000

CMD ["npm", "run", "start"]
