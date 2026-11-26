FROM gcc:14-bookworm AS build
WORKDIR /app
COPY smoke.cc ./
RUN g++ -O2 smoke.cc -o /smoke && ldd /smoke | grep -F libstdc++.so.6

FROM gcr.io/distroless/cc-debian13:nonroot@sha256:29ee70762214fe8190cea04ef1c0a84a8365879e047ce8f911ae5a5ab2355fa2
COPY --from=build /smoke /smoke
ENTRYPOINT ["/smoke"]
