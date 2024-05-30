FROM node:lts-alpine AS api
WORKDIR /usr/share/app
COPY dist/apps/api ./
COPY .env ./
RUN npm i -g pnpm
RUN pnpm i @nestjs/common @nestjs/config @nestjs/core @nestjs/jwt @nestjs/mongoose @nestjs/passport @nestjs/platform-express @nestjs/swagger bcrypt class-transformer class-validator mongoose passport passport-jwt passport-local reflect-metadata rxjs swagger-ui-express tslib zone.js
CMD ["node", "main.js"]

FROM nginx AS app
ADD ["nginx.conf", "/etc/nginx/"]
COPY dist/apps/sick-fansubs/browser /usr/share/nginx/html/
COPY .config.json /usr/share/nginx/html/
COPY .env /usr/share/nginx/html/