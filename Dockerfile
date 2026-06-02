FROM golang:1.26.3 AS builder
WORKDIR /app
RUN curl -sL https://taskfile.dev/install.sh | sh \
  && apt update \
  && apt install -y musl-tools ca-certificates openssh-client \
  && ssh-keyscan github.com gitlab.com >> /etc/ssh/ssh_known_hosts
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN /app/bin/task build

FROM gcr.io/distroless/static-debian13
WORKDIR /
COPY --from=builder /app/dist/composeflux .
COPY --from=builder /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts
ENTRYPOINT ["/composeflux"]
