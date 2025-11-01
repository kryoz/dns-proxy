.PHONY: build build-zip build-release clean

# Директория для итоговых файлов
OUTDIR = dist
BINARY = dns-proxy

build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="-s" -a -o $(OUTDIR)/$(BINARY)-$(GOOS)-$(GOARCH)
	chmod +x $(OUTDIR)/$(BINARY)-$(GOOS)-$(GOARCH)

build-zip:
	zip -j $(OUTDIR)/$(BINARY)-$(GOOS)-$(GOARCH).zip $(OUTDIR)/$(BINARY)-$(GOOS)-$(GOARCH)

# Сборка релиза для всех платформ
build-release:
	@mkdir -p $(OUTDIR)
	$(MAKE) build GOOS=linux GOARCH=amd64
	$(MAKE) build-zip GOOS=linux GOARCH=amd64

	$(MAKE) build GOOS=linux GOARCH=arm64
	$(MAKE) build-zip GOOS=linux GOARCH=arm64

	$(MAKE) build GOOS=linux GOARCH=mipsle
	$(MAKE) build-zip GOOS=linux GOARCH=mipsle

	@echo "✅ Release build completed"

clean:
	rm -rf $(OUTDIR)