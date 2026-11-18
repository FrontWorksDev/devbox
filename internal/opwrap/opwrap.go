// Package opwrap は、1Password CLI（op run）を経由して dev を起動し直すための判定と argv を作る。
//
// dev 自身は秘密の値を扱わない。.env からはキー名と op:// 参照（どの項目か）だけを取り出し、
// 値の解決は op run に任せ、環境変数として dev に渡してもらう。
package opwrap

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// InnerEnv は、op run から起動し直された側であることを示す環境変数。
// これがあれば、承認の確認と op run をもう一度行わない（無限ループ防止）。
const InnerEnv = "DEVBOX_INNER"

// HasOpRef は、.env の内容に op:// 参照があるかを返す。
func HasOpRef(data []byte) bool {
	return bytes.Contains(data, []byte("op://"))
}

// Entry は .env の 1 行。値そのものは持たない。
type Entry struct {
	Name string
	Ref  string // 値が op:// 参照ならその参照（秘密ではないので表示してよい）。それ以外は空
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseEnv は .env のキー名と op:// 参照を取り出す。
//
// op run は .env の行をすべて dev 自身の環境変数にする。何が設定されるかを
// 確かめるために、キー名を読む必要がある。値は参照（op://...）のときだけ返し、
// それ以外の値（直接書かれた秘密かもしれない）は返さず、エラーにも出さない。
func ParseEnv(path string, data []byte) ([]Entry, error) {
	var out []Entry
	// エディタによっては先頭に UTF-8 の BOM が付くので取り除く
	text := strings.TrimPrefix(string(data), "\ufeff")
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: KEY=VALUE の形ではありません", path, i+1)
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if !envNameRe.MatchString(key) {
			return nil, fmt.Errorf("%s:%d: 環境変数名が不正です", path, i+1)
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		e := Entry{Name: key}
		if strings.HasPrefix(val, "op://") {
			e.Ref = val
		}
		out = append(out, e)
	}
	return out, nil
}

// Argv は op run の argv を返す（argv[0] は "op"）。
//
//   - files はプロジェクト → 全体の順で渡すこと。op run は後のファイルを優先するので、
//     プロジェクトの .env で全体のシークレットを上書きされないようにするため。
//   - --no-masking は必須。付けないと op が出力を加工し、Neovim などの対話的なプログラムが壊れる。
func Argv(self string, files []string, args []string) []string {
	argv := []string{"op", "run", "--no-masking"}
	for _, f := range files {
		argv = append(argv, "--env-file="+f)
	}
	argv = append(argv, "--", self)
	return append(argv, args...)
}
