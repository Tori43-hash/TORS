# The bot builder as static files served by Caddy.
#
#   docker build -f deploy/builder.Dockerfile -t tors-builder .
#   docker run -p 8080:80 tors-builder
FROM golang:1.27 AS wasm
WORKDIR /src
COPY . .
RUN GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o /out/tors-theme.wasm ./cmd/ui-wasm \
 && cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" /out/

FROM node:22 AS web
WORKDIR /src
COPY . .
COPY --from=wasm /out/ web/ui-editor/public/
WORKDIR /src/web/ui-editor
RUN npm ci && npx tsc --noEmit && npx vite build

FROM caddy:2
COPY --from=web /src/web/ui-editor/dist /srv
CMD ["caddy", "file-server", "--root", "/srv", "--listen", ":80"]
