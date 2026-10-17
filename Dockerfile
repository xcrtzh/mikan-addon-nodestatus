FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY ui ./ui
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /addon .

FROM scratch
# CA certs for the GitHub API (HTTPS) from the public status page push.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /addon /addon
ENTRYPOINT ["/addon"]
