FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X kbrd/model.Version=${VERSION}" -o /kbrd .

FROM alpine:3.23
# git CLI is a hard runtime requirement (all persistence shells out to git);
# openssh-client is only needed for ssh:// remotes.
RUN apk add --no-cache git ca-certificates openssh-client \
 && adduser -D -u 1000 kbrd
USER kbrd
WORKDIR /board
COPY --from=build /kbrd /usr/local/bin/kbrd
EXPOSE 80 443 8080
ENTRYPOINT ["kbrd", "serve", "--dir", "/board/data"]
