BINARY  := civilsort
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^[^0-9]/0.0.0-&/' || echo 0.0.0-dev)

.PHONY: build clean deb

build:
	go build -o $(BINARY)

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
	dpkg-deb --build dist/deb dist/$(BINARY)_$(VERSION)_amd64.deb

clean:
	rm -rf $(BINARY) dist
