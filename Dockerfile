# Hub image: builds the frontend and the binary inside the image, so the
# embedded frontend always matches the source.
# node stays on the same patch version as mise.toml.
FROM docker.io/library/node:24.22.0-alpine AS web
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
# The image has no .git, so the commit comes in as an argument too.
ARG VERSION=dev
ARG COMMIT=
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/meat-mode ./cmd/meat-mode \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/meat-mode /meat-mode
COPY --from=go --chown=65532:65532 /out/data /data
ENV MEAT_MODE_LISTEN=:8080 MEAT_MODE_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/meat-mode", "hub"]
