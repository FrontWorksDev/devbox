// dev は、プロジェクトのコードを隔離コンテナ（devbox）で動かすための CLI。
// 使い方は dev help、仕様は docs/superpowers/specs/ を参照。
package main

import (
	"os"

	"devbox/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], app.DefaultDeps()))
}
