// Package app は dev のサブコマンドの振り分けと、全体の流れを受け持つ。
package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/approval"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/opwrap"
	"devbox/internal/project"
)

const usage = `使い方:
  dev                 プロジェクトのコンテナに入る（なければ作る）
  dev <command...>    コンテナ内でコマンドを実行   例: dev nvim .   dev claude
  dev -- <command...> stop / build などと同名のコマンドをコンテナ内で実行
  dev stop            このプロジェクトのコンテナを削除（.devbox を変えたら一度これ）
  dev ls              devbox のコンテナの一覧
  dev check           隔離が効いているか、コンテナ内で確認
  dev build [args...] イメージを作る（追加の引数は docker build へ）
  dev help            この説明

設定:
  <project>/.devbox                 このプロジェクトに渡す環境変数名とポート（変わるたびに確認）
  <project>/.env                    値（1Password の op:// 参照）
  ~/.config/devbox/devbox           全プロジェクト共通の環境変数名とポート
  ~/.config/devbox/secrets.env      全プロジェクト共通の値

環境変数:
  DEVBOX_IMAGE     イメージ名（既定 devbox:latest）
  DEVBOX_CONTEXT   dev build で、埋め込みの代わりに使うビルドコンテキスト
`

// Run は dev を実行し、終了コードを返す。
func Run(args []string, d Deps) int {
	if err := run(args, d); err != nil {
		fmt.Fprintf(d.Stderr, "dev: %s\n", err)
		return 1
	}
	return 0
}

func info(d Deps, format string, a ...any) {
	fmt.Fprintf(d.Stderr, "dev: "+format+"\n", a...)
}

func first(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func run(args []string, d Deps) error {
	switch first(args) {
	case "help", "-h", "--help":
		fmt.Fprint(d.Stdout, usage)
		return nil
	}

	dockerPath, err := d.LookPath("docker")
	if err != nil {
		return errors.New("docker が見つかりません")
	}
	client := docker.Client{R: d.Runner, Stderr: d.Stderr}

	switch first(args) {
	case "build":
		return build(d, args[1:])
	case "ls":
		return d.Runner.Run("docker", docker.LsArgs(), d.Stdout, d.Stderr)
	}

	// ここから先はプロジェクトを特定してから動く。危険な場所なら、docker を呼ぶ前に止める。
	cwd, err := d.Getwd()
	if err != nil {
		return err
	}
	home, err := d.UserHomeDir()
	if err != nil {
		return err
	}
	dirs := newDirs(d, home)
	// devbox の設定と承認記録もマウントさせない（コンテナから書き換えられると、承認を偽造できる）
	p, err := project.Resolve(cwd, home, []string{dirs.config, dirs.state}, func(dir string) (string, error) {
		var out bytes.Buffer
		err := d.Runner.Run("git", []string{"-C", dir, "rev-parse", "--show-toplevel"}, &out, nil)
		return out.String(), err
	})
	if err != nil {
		return err
	}

	if first(args) == "stop" {
		return stop(d, client, p)
	}

	cmd := args
	switch first(args) {
	case "check":
		cmd = []string{"devbox-check"}
	case "--":
		cmd = args[1:]
	}
	if len(cmd) == 0 {
		cmd = []string{"bash", "-l"}
	}

	replaced, err := ensureContainer(d, client, p, home, dirs, args)
	if err != nil {
		return err
	}
	if replaced {
		// op run に置き換えた（本物の Exec なら、ここには来ない）。
		// 偽物の Exec のときに、そのままコンテナ作成や exec に進まないよう、ここで終える。
		return nil
	}

	// 作業フォルダは、シンボリックリンクを解決した cwd から決める（Root も解決済みなので）
	realCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		realCwd = cwd
	}
	argv := append([]string{"docker"},
		docker.ExecArgs(p.ContainerName, p.Workdir(realCwd), d.IsTerminal(), cmd)...)
	// docker に置き換え、Neovim や Claude Code に端末をそのまま渡す。
	if err := d.Exec(dockerPath, argv, d.Environ()); err != nil {
		return fmt.Errorf("docker exec を起動できませんでした: %w", err)
	}
	// 本物の Exec は戻らない。偽物のときに、ここで止める。
	return nil
}

// dirs は devbox の設定と状態のフォルダ。
type dirs struct {
	config string // $XDG_CONFIG_HOME/devbox
	state  string // $XDG_STATE_HOME/devbox
}

func newDirs(d Deps, home string) dirs {
	return dirs{
		config: filepath.Join(lookup(d, "XDG_CONFIG_HOME", filepath.Join(home, ".config")), "devbox"),
		state:  filepath.Join(lookup(d, "XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "devbox"),
	}
}

// settings は、コンテナを作るときに読む設定一式。
type settings struct {
	devboxPath string
	devbox     []byte // プロジェクトの .devbox（なければ nil）
	projCfg    config.Config
	globalCfg  config.Config

	envPath    string
	env        []byte         // プロジェクトの .env（なければ nil）
	envEntries []opwrap.Entry // .env のキー名と参照
	secrets    []byte         // 全体の secrets.env（なければ nil）
	secretsPth string
}

// loadSettings は設定を読み、.env が dev を乗っ取れないことを確かめる。
//
// op run は .env の行をすべて、起動し直した dev 自身の環境変数にする。
// そこに DOCKER_HOST や PATH を書かれると、dev が攻撃者の docker に秘密を送ったり、
// 偽の docker を実行したりしてしまう。そのため .env に書ける名前は、
// 設定（.devbox / devbox）で列挙した名前で、かつ制御用ではないものに限る。
func loadSettings(p project.Project, dd dirs) (settings, error) {
	s := settings{
		devboxPath: filepath.Join(p.Root, ".devbox"),
		envPath:    filepath.Join(p.Root, ".env"),
		secretsPth: filepath.Join(dd.config, "secrets.env"),
	}
	var err error
	// プロジェクトの中のファイルは、リポジトリやコンテナが自由に作れるので、シンボリックリンクを拒否する
	if s.devbox, _, err = config.ReadFile(s.devboxPath, true); err != nil {
		return s, err
	}
	if s.devbox != nil {
		// 承認の画面に出す内容と、記録するハッシュを、同じ読み込みから作る
		if s.projCfg, err = config.Parse(s.devboxPath, bytes.NewReader(s.devbox)); err != nil {
			return s, err
		}
	}
	if s.globalCfg, _, err = config.Load(filepath.Join(dd.config, "devbox")); err != nil {
		return s, err
	}
	if s.env, _, err = config.ReadFile(s.envPath, true); err != nil {
		return s, err
	}
	if s.secrets, _, err = config.ReadFile(s.secretsPth, false); err != nil {
		return s, err
	}

	listed := map[string]bool{}
	for _, n := range config.Merge(s.projCfg, s.globalCfg).Envs {
		listed[n] = true
	}
	if s.env != nil {
		if s.envEntries, err = opwrap.ParseEnv(s.envPath, s.env); err != nil {
			return s, err
		}
		for _, e := range s.envEntries {
			if config.Denied(e.Name) {
				return s, fmt.Errorf("%s: %s は .env に書けません（dev や docker の動作を変えられるため）", s.envPath, e.Name)
			}
			if !listed[e.Name] {
				return s, fmt.Errorf("%s: %s は .devbox にも全体設定にも列挙されていない名前です", s.envPath, e.Name)
			}
		}
	}
	if s.secrets != nil {
		// 全体の secrets.env はホストのファイルだが、制御用の名前は同じ理由で拒否する
		entries, err := opwrap.ParseEnv(s.secretsPth, s.secrets)
		if err != nil {
			return s, err
		}
		for _, e := range entries {
			if config.Denied(e.Name) {
				return s, fmt.Errorf("%s: %s は書けません（dev や docker の動作を変えられるため）", s.secretsPth, e.Name)
			}
		}
	}
	return s, nil
}

// opEnvFiles は op run に渡すファイルを、プロジェクト → 全体の順で返す。
// op run は後のファイルを優先するので、プロジェクトの .env で全体のシークレットを上書きされないようにするため。
func (s settings) opEnvFiles() []string {
	var files []string
	if s.env != nil && opwrap.HasOpRef(s.env) {
		files = append(files, s.envPath)
	}
	if s.secrets != nil && opwrap.HasOpRef(s.secrets) {
		files = append(files, s.secretsPth)
	}
	return files
}

// ensureContainer はコンテナを用意する。op run に置き換えたときは replaced = true。
func ensureContainer(d Deps, client docker.Client, p project.Project, home string, dd dirs, args []string) (replaced bool, err error) {
	st, layout, err := client.Inspect(p.ContainerName)
	if err != nil {
		return false, err
	}
	if st != docker.Missing {
		// 既存のコンテナには、作成時に渡したものが使われる。設定の読み込みや承認はしない。
		return false, useExisting(client, p, st, layout)
	}

	s, err := loadSettings(p, dd)
	if err != nil {
		return false, err
	}

	// op run から起動し直された側（inner）では、確認も op run もしない（無限ループ防止）。
	// ただし承認記録の照合は inner でも行う。DEVBOX_INNER を外から設定されても、承認を回避できないように。
	_, inner := d.LookupEnv(opwrap.InnerEnv)
	// 確認は op run より前に行う。1Password のロックを解除する前に、何を渡すかを見せるため。
	if err := approve(d, approval.Store{Dir: filepath.Join(dd.state, "approved")}, p, s, !inner); err != nil {
		return false, err
	}
	if !inner {
		if files := s.opEnvFiles(); len(files) > 0 {
			opPath, err := d.LookPath("op")
			if err != nil {
				info(d, "注意: op が見つからないので、1Password の参照（op://）は読み込みません")
			} else {
				self, err := d.Executable()
				if err != nil {
					return false, err
				}
				info(d, "1Password からシークレットを読み込みます")
				env := append(d.Environ(), opwrap.InnerEnv+"=1")
				if err := d.Exec(opPath, opwrap.Argv(self, files, args), env); err != nil {
					return false, fmt.Errorf("op run を起動できませんでした: %w", err)
				}
				return true, nil
			}
		}
	}

	image := imageName(d)
	if !client.ImageExists(image) {
		return false, fmt.Errorf("イメージ %s がありません。先に dev build を実行してください", image)
	}
	_, gitErr := os.Stat(filepath.Join(p.Root, ".git"))
	if gitErr != nil {
		info(d, "注意: .git がありません。リポジトリはホスト側で git init / clone してください")
	}
	nvim, nvimErr := os.Stat(filepath.Join(home, ".config", "nvim"))

	cfg := config.Merge(s.projCfg, s.globalCfg)
	var envs []string
	passWaka := false
	for _, name := range cfg.Envs {
		// 空文字で設定されているものは「設定されている」とみなして渡す
		if _, ok := d.LookupEnv(name); !ok {
			info(d, "注意: 環境変数 %s がホスト側で設定されていないので渡しません", name)
			continue
		}
		envs = append(envs, name)
		if name == "WAKATIME_API_KEY" {
			passWaka = true
		}
	}

	info(d, "コンテナ %s を作成します", p.ContainerName)
	err = client.Create(docker.RunSpec{
		Name:          p.ContainerName,
		ProjectRoot:   p.Root,
		ContainerPath: p.ContainerPath,
		Home:          home,
		Image:         image,
		HasGit:        gitErr == nil,
		HasNvimConfig: nvimErr == nil && nvim.IsDir(),
		Envs:          envs,
		Ports:         cfg.Ports,
	})
	if err != nil {
		// cmux でペインを同時に開くと、別のペインが先に同じ名前で作っていることがある。
		// そのときは失敗とせず、そのコンテナを使う。
		if st, layout, ierr := client.Inspect(p.ContainerName); ierr == nil && st != docker.Missing {
			info(d, "別のペインで作られた %s を使います", p.ContainerName)
			return false, useExisting(client, p, st, layout)
		}
		return false, fmt.Errorf("コンテナ %s を作成できませんでした: %w", p.ContainerName, err)
	}
	if passWaka {
		if err := client.SetupWakaTime(p.ContainerName); err != nil {
			info(d, "注意: WakaTime の設定を書けませんでした: %s", err)
		}
	}
	return false, nil
}

// useExisting は既存のコンテナを使えるようにする。古い layout ならエラー、停止中なら起動する。
func useExisting(client docker.Client, p project.Project, st docker.State, layout string) error {
	if layout != docker.LayoutVersion {
		return fmt.Errorf("%s は古い dev で作られています。dev stop してから、もう一度実行してください", p.ContainerName)
	}
	if st == docker.Stopped {
		if err := client.Start(p.ContainerName); err != nil {
			return fmt.Errorf("%s を起動できませんでした: %w", p.ContainerName, err)
		}
	}
	return nil
}

// approve は、プロジェクトの .devbox と .env が承認済みかを確かめる。
// 承認済みでなく ask のときは利用者に確認し、承認されたら記録する。ask でなければエラーにする。
// どちらのファイルもなければ、確認は要らない（全体設定はホストのファイルなので確認しない）。
func approve(d Deps, store approval.Store, p project.Project, s settings, ask bool) error {
	if s.devbox == nil && s.env == nil {
		return nil
	}
	sum := approval.Fingerprint(s.devbox, s.env)
	if store.IsApproved(p.Key, sum) {
		return nil
	}
	var files []string
	if s.devbox != nil {
		files = append(files, s.devboxPath)
	}
	if s.env != nil {
		files = append(files, s.envPath)
	}
	if !ask {
		return fmt.Errorf("%s が承認されていません", strings.Join(files, " と "))
	}
	tty, err := d.OpenTTY()
	if err != nil {
		return fmt.Errorf("%s を確認できない環境です。対話できるターミナルで実行してください", strings.Join(files, " と "))
	}
	defer tty.Close()
	if !approval.Ask(tty, d.Stderr, files, s.projCfg, s.envEntries) {
		return errors.New("中止しました")
	}
	return store.Record(p.Key, sum)
}

func lookup(d Deps, key, def string) string {
	if v, ok := d.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func imageName(d Deps) string {
	return lookup(d, "DEVBOX_IMAGE", "devbox:latest")
}

// build はイメージを作る。ビルドコンテキストは、バイナリに埋め込んだものを一時フォルダに書き出して使う。
func build(d Deps, extra []string) error {
	ctxDir := lookup(d, "DEVBOX_CONTEXT", "")
	if ctxDir == "" {
		tmp, err := os.MkdirTemp("", "devbox-build-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		if err := d.WriteBuildContext(tmp); err != nil {
			return err
		}
		ctxDir = tmp
	}
	args := docker.BuildArgs(imageName(d), d.Getuid(), d.Getgid(), extra, ctxDir)
	if err := d.Runner.Run("docker", args, d.Stdout, d.Stderr); err != nil {
		return fmt.Errorf("docker build に失敗しました: %w", err)
	}
	return nil
}

func stop(d Deps, client docker.Client, p project.Project) error {
	st, _, err := client.Inspect(p.ContainerName)
	if err != nil {
		return err
	}
	if st == docker.Missing {
		info(d, "%s はありません", p.ContainerName)
		return nil
	}
	if err := client.Remove(p.ContainerName); err != nil {
		return fmt.Errorf("%s を削除できませんでした: %w", p.ContainerName, err)
	}
	info(d, "%s を削除しました", p.ContainerName)
	return nil
}
