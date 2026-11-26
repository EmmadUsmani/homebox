FROM public.ecr.aws/docker/library/node:22-alpine
RUN apk add --no-cache postgresql-client redis
COPY server.cjs /app/server.cjs
USER node
