VERSION=$(node -p "require('./package.json').version")
mkdir -p bin
echo go v$VERSION
echo "building go binary..."
go build -ldflags "-X github.com/ekkolyth/dump/internal/version.Version=$VERSION" -o bin/dump ./cmd/dump
echo "built sucessfully"
