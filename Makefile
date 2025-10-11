BINARY=us3ui
BUILD_TIME=`date +%FT%T%z`

default: build

clean:
	rm -rf ./bin

build:
	CGO_CFLAGS="-I/home/linuxbrew/.linuxbrew/opt/libxi/include -I/home/linuxbrew/.linuxbrew/opt/libxinerama/include -I/home/linuxbrew/.linuxbrew/opt/libxfixes/include -I/home/linuxbrew/.linuxbrew/opt/libxrender/include -I/home/linuxbrew/.linuxbrew/opt/libxcursor/include -I/home/linuxbrew/.linuxbrew/opt/libx11/include -I/home/linuxbrew/.linuxbrew/opt/libxext/include -I/home/linuxbrew/.linuxbrew/opt/xorgproto/include -I/home/linuxbrew/.linuxbrew/opt/libxrandr/include  -I/home/linuxbrew/.linuxbrew/opt/mesa/include" \
	CGO_LDFLAGS="-L/home/linuxbrew/.linuxbrew/opt/libxxf86vm/lib -L/home/linuxbrew/.linuxbrew/opt/libxi/lib -L/home/linuxbrew/.linuxbrew/opt/libxinerama/lib -L/home/linuxbrew/.linuxbrew/opt/libxfixes/lib -L/home/linuxbrew/.linuxbrew/opt/libxrender/lib -L/home/linuxbrew/.linuxbrew/opt/libxcursor/lib -L/home/linuxbrew/.linuxbrew/opt/libx11/lib -L/home/linuxbrew/.linuxbrew/opt/libxext/lib -L/home/linuxbrew/.linuxbrew/opt/xorgproto/lib -L/home/linuxbrew/.linuxbrew/opt/libxrandr/lib  -L/home/linuxbrew/.linuxbrew/opt/mesa/lib" \
	PKG_CONFIG_PATH="/home/linuxbrew/.linuxbrew/lib/pkgconfig" \
	go build -a -o ./bin/${BINARY}-${BUILD_TIME} *.go

build-linux:
	GOARCH=amd64 \
	GOOS=linux \
	go build -ldflags "-X main.Version=${BUILD_TIME}" -a -o ./bin/${BINARY}-${BUILD_TIME} *.go

deps:
	go mod tidy;

test:
	go test
