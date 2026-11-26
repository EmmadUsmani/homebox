FROM public.ecr.aws/docker/library/alpine:3.22 AS build
ARG TARGETARCH
ARG CACHE_PHASE=write
RUN --mount=type=cache,id=homebox-smoke,target=/cache \
    if [ "$CACHE_PHASE" = write ]; then touch "/cache/$TARGETARCH"; \
    else test -f "/cache/$TARGETARCH"; fi && \
    printf '%s:%s\n' "$TARGETARCH" "$(uname -m)" > /result

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /result /result
