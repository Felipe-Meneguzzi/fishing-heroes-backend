# ---- build ----
FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -o /out/api ./cmd/api
RUN go build -trimpath -o /out/migrate ./cmd/migrate

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/migrate /app/migrate
COPY migrations /app/migrations
EXPOSE 8080
# CMD (não ENTRYPOINT) para o docker-compose poder substituir por /app/migrate.
CMD ["/app/api"]
