BINARY=us3ui
VERSION=v1.4.0
BUILD_TIME=`date +%FT%T%z`
GOX_OSARCH="darwin/amd64 darwin/arm64 linux/386 linux/amd64 windows/386 windows/amd64"

default: build

clean:
	rm -rf ./bin

build:
	go build -a -o ./bin/${BINARY}-${VERSION} *.go

build-linux:
	GOARCH=amd64 \
	GOOS=linux \
	go build -ldflags "-X main.Version=${VERSION}" -a -o ./bin/${BINARY}-${VERSION} *.go

build-gox:
	gox -ldflags "-X main.Version=${VERSION}" -osarch=${GOX_OSARCH} -output="bin/${VERSION}/{{.Dir}}_{{.OS}}_{{.Arch}}"

deps:
	dep ensure;

test:
	go test

fynetools:
	go install fyne.io/tools/cmd/fyne@latest
	go install github.com/fyne-io/fyne-cross@latest