FROM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY api/openapi.yaml /src/api/openapi.yaml
COPY web ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/assets/app ./assets/app
COPY --from=web /src/assets/js ./assets/js
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /prospector .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /prospector /prospector
ENV PORT=8080 TZ=Europe/London
EXPOSE 8080
ENTRYPOINT ["/prospector", "serve"]
