FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /zombiewatch .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /zombiewatch /zombiewatch
USER 65532:65532
ENTRYPOINT ["/zombiewatch"]
