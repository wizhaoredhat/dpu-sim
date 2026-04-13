FROM golang:1.25 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /dpu-sim-dp ./cmd/dpu-sim-dp

FROM gcr.io/distroless/static
COPY --from=builder /dpu-sim-dp /dpu-sim-dp
ENTRYPOINT ["/dpu-sim-dp"]
