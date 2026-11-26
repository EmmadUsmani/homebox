FROM --platform=$BUILDPLATFORM maven:3.9.11-eclipse-temurin-21 AS build
WORKDIR /app
COPY java/ ./
RUN mvn -B -ntp package

FROM gcr.io/distroless/java21-debian13:nonroot@sha256:128bd878463537eef7771b50fecf68faace9089f654a2d233f1c4f344e6817d9
COPY --from=build /app/target/smoke.jar /app/smoke.jar
CMD ["/app/smoke.jar"]
