VERSION=$(node -p "require('./package.json').version")
mkdir -p bin
go build -ldflags "-X github.com/ekkolyth/dump/internal/version.Version=$VERSION" -o bin/dump ./cmd/dump
