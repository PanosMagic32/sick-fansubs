FROM node:lts-alpine AS api
WORKDIR /usr/share/app
COPY ["dist/apps/api", "package.json", "pnpm-lock.json", "decorate-angular-cli.js", ".env", "./"] 
RUN npm ci
CMD ["node", "main.js"]

FROM nginx AS app
ADD ["nginx.conf", "/etc/nginx/"]
COPY ["dist/apps/sick-fansubs", ".config.json", ".env", "/usr/share/nginx/html/"]