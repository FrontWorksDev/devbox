BIN := bin/sigil
INSTALL_PATH := $(HOME)/.local/bin/sigil
# 改名前のコマンド名。互換は残さないので、install のときに消す
OLD_PATH := $(HOME)/.local/bin/dev

.PHONY: build install test vet fmt check

build:
	CGO_ENABLED=0 go build -trimpath -o $(BIN) .

# 置き場所にシンボリックリンクがあると、install はリンク先を上書きしてしまうので、
# 先にリンクだけを消してから実体を置く。
install: build
	mkdir -p $(dir $(INSTALL_PATH))
	rm -f $(INSTALL_PATH)
	install -m 0755 $(BIN) $(INSTALL_PATH)
	rm -f $(OLD_PATH)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt が必要: $$out"; exit 1; fi

check: test vet fmt
