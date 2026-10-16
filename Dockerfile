# Hub image: builds the frontend and the binary inside the image, so the
# embedded frontend always matches the source.
FROM docker.io/library/node:24-alpine AS web
WORKDIR /src
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/
RUN cd web && pnpm install --frozen-lockfile
COPY web/ web/
RUN cd web && pnpm build

FROM docker.io/library/golang:1.27-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/static/ web/static/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/meat-mode ./cmd/meat-mode \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/meat-mode /meat-mode
COPY --from=go --chown=65532:65532 /out/data /data
ENV MEAT_MODE_LISTEN=:8080 MEAT_MODE_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/meat-mode", "hub"]
