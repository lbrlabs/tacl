# syntax=docker/dockerfile:1

FROM cgr.dev/chainguard/go:latest AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Copy the remaining project files
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /src/tacl .

FROM cgr.dev/chainguard/static:latest

# Default to port 8080, but it can be overridden at runtime
ENV TACL_PORT=8080

# Copy the binary from the builder stage
COPY --from=builder /src/tacl /usr/local/bin/tacl

EXPOSE 8080

# Launch
ENTRYPOINT ["/usr/local/bin/tacl"]

CMD ["serve"]
