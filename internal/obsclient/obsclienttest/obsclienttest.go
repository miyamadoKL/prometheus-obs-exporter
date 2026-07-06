// Package obsclienttest は、internal/obsclient 自身のブラックボックステストと
// internal/collector のテスト（フィクスチャサーバーに裏打ちされた実際の、
// 認証済み obsclient.Client を必要とする）の両方で使われる共有 httptest
// フィクスチャサーバーの配線を提供する。この配線を両パッケージ間で
// 重複させないために存在する。
//
// internal/obsclient/auth_test.go は意図的にこのパッケージを使わない:
// これは非公開の authToken フィールドに対してアサートするホワイトボックス
// （package obsclient）テストであり、パッケージ内テストファイルは
// obsclient 自身をインポートするパッケージをインポートできない
// （import cycle になる）。代わりに ServeFixture/NewTestClient の
// 小さな非公開コピーを持つ（internal/obsclient/testhelpers_test.go 参照）。
package obsclienttest

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
)

// AuthTokenHeader は obsclient の非公開の authTokenHeader 定数
// （internal/obsclient/auth.go）に対応する。インポートできないためここで
// 複製している。
const AuthTokenHeader = "X-SDS-AUTH-TOKEN"

// Username と Password は、フィクスチャサーバーが期待してよい認証情報で、
// NewTestClient が Client に設定するのもこの値。
const (
	Username = "monitor"
	Password = "s3cr3t"
)

// ServeFixture は testdata/name の内容（実行中のテストバイナリの作業
// ディレクトリ、つまりテスト対象パッケージのディレクトリからの相対パスで
// 解決される）を JSON レスポンス body として書き込む。
func ServeFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %q: %v", name, err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(data); err != nil {
		t.Fatalf("writing fixture %q: %v", name, err)
	}
}

// NewTestClient は httptest.NewTLSServer サーバーを指す obsclient.Client を
// TLS 検証を無効にして構築する（テストサーバーは自己署名証明書を使うため）。
// これにより（認証を含む）実際のリクエスト/レスポンスの配線がエンドツー
// エンドで実行される。
func NewTestClient(t *testing.T, serverURL, username, password string) *obsclient.Client {
	t.Helper()

	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parsing test server URL %q: %v", serverURL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing test server port from %q: %v", serverURL, err)
	}

	c, err := obsclient.New(u.Hostname(), obsclient.Config{
		Username:              username,
		Password:              password,
		MgmtPort:              port,
		TLSInsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("obsclient.New: %v", err)
	}
	return c
}
