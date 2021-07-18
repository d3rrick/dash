FROM golang:alpine as builder
LABEL maintainer="d-gopher"
WORKDIR /build
COPY . ./
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-extldflags "-static"' -o main .
FROM alpine
WORKDIR /build
COPY --from=builder /build/main /build/
COPY configs.yaml configs.yaml
CMD ["./main -c ${CONFIG} -s ${SCENARIOS}"]