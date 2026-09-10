FROM node:24-alpine
LABEL org.opencontainers.image.authors="jecklgamis@gmail.com"

RUN apk add --no-cache curl dumb-init

ENV APP_HOME=/app

RUN mkdir -p ${APP_HOME}
WORKDIR /app
COPY "package.json" .
COPY "package-lock.json" .
RUN npm install -production
ADD public public
ADD routes routes
ADD views views
ADD middleware middleware
COPY app.js .
COPY build-info-data.js .
COPY server.crt .
COPY server.key .

RUN addgroup -S app && adduser -S app -G app
RUN chown -R app:app /app

EXPOSE 38080
EXPOSE 8443
COPY docker-entrypoint.sh /
RUN chmod +x /docker-entrypoint.sh

ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ["/docker-entrypoint.sh"]
