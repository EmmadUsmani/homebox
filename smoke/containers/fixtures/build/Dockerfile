FROM alpine:3.22 AS build
ARG TARGETARCH
ARG CACHE_PHASE=write
ARG CACHE_ID=homebox-smoke
RUN --mount=type=cache,id=$CACHE_ID,target=/cache \
    if [ "$CACHE_PHASE" = write ]; then touch "/cache/$TARGETARCH"; \
    else test -f "/cache/$TARGETARCH"; fi && \
    printf '%s:%s\n' "$TARGETARCH" "$(uname -m)" > /result

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /result /result
