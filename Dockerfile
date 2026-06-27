# Build a static linux/amd64 xenond binary (templates are go:embed'd, no deps).
FROM golang:1.23 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /xenond ./cmd/xenond

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /xenond /xenond
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/xenond"]
