package opwrap

import (
	"reflect"
	"strings"
	"testing"
)

func TestHasOpRef(t *testing.T) {
	if !HasOpRef([]byte("X=op://vault/item/field\n")) {
		t.Error("op:// を見つけない")
	}
	if HasOpRef([]byte("Z=literal\n")) {
		t.Error("op:// がないのに true")
	}
}

func TestParseEnv(t *testing.T) {
	in := strings.Join([]string{
		"# comment",
		"",
		"DATABASE_URL=op://dev/db/url",
		`export STRIPE_KEY="op://dev/stripe/secret"`,
		"PLAIN=literal-secret-value",
		"  SPACED = op://dev/x/y  ",
		"CRLF=op://dev/c/d\r",
	}, "\n")
	got, err := ParseEnv("/p/.env", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Name: "DATABASE_URL", Ref: "op://dev/db/url"},
		{Name: "STRIPE_KEY", Ref: "op://dev/stripe/secret"},
		{Name: "PLAIN", Ref: ""}, // 参照ではない値は持たない
		{Name: "SPACED", Ref: "op://dev/x/y"},
		{Name: "CRLF", Ref: "op://dev/c/d"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseEnv =\n%v\nwant\n%v", got, want)
	}
}

func TestParseEnvStripsBOM(t *testing.T) {
	got, err := ParseEnv("/p/.env", []byte("\xef\xbb\xbfA=op://v/i/f\n"))
	if err != nil || len(got) != 1 || got[0].Name != "A" {
		t.Errorf("ParseEnv = %v, %v", got, err)
	}
}

func TestParseEnvErrorsDoNotLeakValues(t *testing.T) {
	cases := map[string]string{
		"no-equals-secret-value\n": "/p/.env:1: KEY=VALUE の形ではありません",
		"1BAD=secret-value\n":      "/p/.env:1: 環境変数名が不正です",
		"A=1\n$(id)=secret\n":      "/p/.env:2: 環境変数名が不正です",
	}
	for in, want := range cases {
		_, err := ParseEnv("/p/.env", []byte(in))
		if err == nil || err.Error() != want {
			t.Errorf("ParseEnv(%q) = %v, want %q", in, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Errorf("値がエラーに出た: %v", err)
		}
	}
}

func TestArgv(t *testing.T) {
	got := Argv("/abs/dev", []string{"/p/.env", "/c/secrets.env"}, []string{"nvim", "."})
	want := []string{"op", "run", "--no-masking",
		"--env-file=/p/.env", "--env-file=/c/secrets.env",
		"--", "/abs/dev", "nvim", "."}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Argv =\n%v\nwant\n%v", got, want)
	}
}
