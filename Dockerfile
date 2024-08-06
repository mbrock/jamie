FROM golang:1.22

WORKDIR /src
RUN apt-get update && apt-get install -y \
    libopusenc-dev opus-tools libonnx-dev
RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
RUN go install github.com/a-h/templ/cmd/templ@latest
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make

