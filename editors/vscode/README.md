# Arkadia for Visual Studio Code

- `.ark` ファイルのシンタックスハイライト
- `ark lsp` による診断（型エラー）、ホバーでの型表示、定義へジャンプ

## 使い方

```sh
cd editors/vscode
npm install
# VS Code でこのフォルダを開き F5、または vsce package で .vsix を作成
```

`ark` が `PATH` に無い場合は設定 `arkadia.serverPath` で実行ファイルを指定してください。
