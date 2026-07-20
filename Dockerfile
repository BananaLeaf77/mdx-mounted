# Stage 1: Build
FROM golang:1.25-alpine AS builder

# Set working directory di dalam kontainer
WORKDIR /app

# Install dependencies yang dibutuhkan untuk build (opsional, jika pakai CGO)
# RUN apk add --no-cache gcc musl-dev

# Copy go mod dan sum files
COPY go.mod go.sum ./

# Download semua dependencies
RUN go mod download

# Copy seluruh source code
COPY . .

# Build aplikasi (Nonaktifkan CGO agar binary bisa jalan di scratch/alpine murni)
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Stage 2: Final Image
FROM alpine:latest  

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy binary dari stage builder
COPY --from=builder /app/main .

# Copy invoice logo used by PDF generation
COPY --from=builder /app/invoice_logo.jpg .

EXPOSE 8080

CMD ["./main"]
