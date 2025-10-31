build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s" -a -o dns-proxy

build:
	CGO_ENABLED=0 go build -ldflags="-s" -a -o dns-proxy