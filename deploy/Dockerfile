# The bot from a bot.json made in the builder, with exactly the modules it lists.
#
#   docker build -f deploy/Dockerfile --build-arg BOT=deploy/bot.json -t my-bot .
#
# The build context is the repository root: the core is taken from it
# (tors build --replace), third-party modules are downloaded at the versions
# pinned in bot.json.
FROM golang:1.27 AS build
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG BOT=deploy/bot.json
RUN go run ./cmd/tors build --config "$BOT" --replace . -o /out/bot \
 && cp "$BOT" /out/bot.json \
 && /out/bot version >/dev/null

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bot /bot
COPY --from=build /out/bot.json /etc/tors/bot.json
ENTRYPOINT ["/bot", "run", "--config", "/etc/tors/bot.json"]
