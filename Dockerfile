FROM node:lts-alpine AS api
WORKDIR /usr/share/app
COPY dist/apps/api ./
COPY package.json ./
COPY pnpm-*.yaml ./
COPY .env ./
RUN npm i -g pnpm
RUN pnpm i
CMD ["node", "main.js"]

FROM nginx AS app
ADD ["nginx.conf", "/etc/nginx/"]
COPY dist/apps/web/browser /usr/share/nginx/html/
COPY .config.json /usr/share/nginx/html/
COPY .env /usr/share/nginx/html/
