BINARY  := civilsort
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo 0.0.0-dev)

.PHONY: build web generate lint check clean deb

build: web
	go build -o $(BINARY)

node_modules: package.json package-lock.json
	npm ci
	@touch node_modules

web: node_modules
	npm run build

generate:
	go generate ./...

lint: node_modules
	golangci-lint run ./...
	go tool nilaway -include-pkgs=github.com/civilfritz/civilsort ./...
	npm run typecheck

check: lint
	go test -race ./...

deb: build
	mkdir -p dist/deb/usr/bin
	mkdir -p dist/deb/lib/systemd/system
	mkdir -p dist/deb/etc/default
	mkdir -p dist/deb/DEBIAN
	cp $(BINARY) dist/deb/usr/bin/
	cp debian/civilsort.service dist/deb/lib/systemd/system/
	cp debian/civilsort.env dist/deb/etc/default/civilsort
	sed 's/VERSION_PLACEHOLDER/$(VERSION)/' debian/control > dist/deb/DEBIAN/control
	cp debian/postinst dist/deb/DEBIAN/
	chmod 755 dist/deb/DEBIAN/postinst
	echo "/etc/default/civilsort" > dist/deb/DEBIAN/conffiles
	dpkg-deb --build dist/deb dist/$(BINARY)_$(VERSION)_amd64.deb

clean:
	rm -rf $(BINARY) dist static/app.js static/app.js.map
