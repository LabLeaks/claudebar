BINARY := claudebar
PREFIX := $(HOME)/.local/bin

.PHONY: build install clean

build:
	go build -ldflags="-s -w -X main.version=dev" -o $(BINARY) .

install: build
	mkdir -p $(PREFIX)
	rm -f $(PREFIX)/$(BINARY)
	cp $(BINARY) $(PREFIX)/$(BINARY)

clean:
	rm -f $(BINARY)
