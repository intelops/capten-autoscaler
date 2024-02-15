FROM golang as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY ./ ./
RUN go mod download

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o autoscaler main.go

FROM scratch
WORKDIR /
COPY --from=builder /workspace/autoscaler .

ENTRYPOINT ["/autoscaler"]
