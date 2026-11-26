# output.css and the built app are committed, so this is a plain Go build.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /prospector .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /prospector /prospector
ENV PORT=8080 TZ=Europe/London
EXPOSE 8080
ENTRYPOINT ["/prospector", "serve"]
