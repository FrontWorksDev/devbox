BIN := bin/dev
INSTALL_PATH := $(HOME)/.local/bin/dev

.PHONY: build install test vet fmt check

build:
	CGO_ENABLED=0 go build -trimpath -o $(BIN) .

# ~/.local/bin/dev はシェル版へのシンボリックリンクのことがある。
# install はリンクをたどってリンク先（dev.sh）を上書きしてしまうので、
# 先にリンクだけを消してから実体を置く。
install: build
	mkdir -p $(dir $(INSTALL_PATH))
	rm -f $(INSTALL_PATH)
	install -m 0755 $(BIN) $(INSTALL_PATH)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt が必要: $$out"; exit 1; fi

check: test vet fmt
