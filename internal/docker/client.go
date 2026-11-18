package docker

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner は外部コマンドを実行する。終了コードが 0 以外ならエラーを返す。
// テストでは偽物に差し替え、docker がなくても動くようにする。
type Runner interface {
	Run(name string, args []string, stdout, stderr io.Writer) error
}

// ExecRunner は os/exec で実際にコマンドを実行する Runner。
// 環境変数はそのまま子プロセスに引き継がれる（docker run -e NAME が値を読むため）。
type ExecRunner struct{}

func (ExecRunner) Run(name string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// State はコンテナの状態。
type State int

const (
	Missing State = iota
	Running
	Stopped
)

// Client は docker の操作をまとめる。
type Client struct {
	R      Runner
	Stderr io.Writer // docker の標準エラーを見せる先。nil なら捨てる
}

func (c Client) run(stdout io.Writer, args ...string) error {
	return c.R.Run("docker", args, stdout, c.Stderr)
}

// Inspect はコンテナの状態と、layout ラベルの値を返す。
//
// docker container inspect は、コンテナがないときも、docker が使えないときも、
// 空行を出力して失敗する。出力で判定すると「存在する」と誤判定し（シェル版で実際に起きた不具合）、
// 失敗をすべて「ない」とすると、Docker Desktop が止まっているときに誤った案内になる。
// そこで docker ps を使う。コンテナがなければ成功して何も出力せず、docker の異常は終了コードで分かる。
func (c Client) Inspect(name string) (State, string, error) {
	var out bytes.Buffer
	args := []string{"ps", "-a", "--filter", "name=" + name,
		"--format", "{{.Names}}\t{{.State}}\t{{.Label \"" + LayoutLabel + "\"}}"}
	if err := c.R.Run("docker", args, &out, nil); err != nil {
		return Missing, "", fmt.Errorf("docker に接続できません。Docker Desktop が起動しているか確認してください（%w）", err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Split(line, "\t")
		// name フィルタは部分一致なので、名前が完全に一致する行だけを見る
		if len(fields) != 3 || fields[0] != name {
			continue
		}
		if fields[1] == "running" {
			return Running, fields[2], nil
		}
		return Stopped, fields[2], nil
	}
	return Missing, "", nil
}

// ImageExists はイメージがあるかを返す。
func (c Client) ImageExists(image string) bool {
	return c.R.Run("docker", []string{"image", "inspect", image}, nil, nil) == nil
}

// Create はコンテナを作る。標準出力（コンテナ ID）は捨て、標準エラーは見せる。
func (c Client) Create(s RunSpec) error {
	return c.run(nil, RunArgs(s)...)
}

// Start は停止中のコンテナを起動する。
func (c Client) Start(name string) error {
	return c.run(nil, "start", name)
}

// Remove はコンテナを削除する。
// docker rm -f は、版によっては存在しないコンテナでも成功するので、
// 「なかった」かどうかは呼ぶ側が Inspect で判定する。
func (c Client) Remove(name string) error {
	return c.run(nil, "rm", "-f", name)
}

// SetupWakaTime は、コンテナに WAKATIME_API_KEY が渡っていれば ~/.wakatime.cfg を作る。
// キーはコンテナ内の環境変数から読ませ、ホストのコマンドラインには載せない。
func (c Client) SetupWakaTime(name string) error {
	if c.run(nil, "exec", name, "sh", "-c", `[ -n "${WAKATIME_API_KEY:-}" ]`) != nil {
		return nil
	}
	return c.run(nil, "exec", name, "sh", "-c",
		`umask 077; printf "[settings]\napi_key = %s\n" "$WAKATIME_API_KEY" > "$HOME/.wakatime.cfg"`)
}
