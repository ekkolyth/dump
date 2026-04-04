echo "Building dump..."
mkdir -p bin
go build -o bin/dump ./cmd/dump
echo "Starting dump..."
./bin/dump
