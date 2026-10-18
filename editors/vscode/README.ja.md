# Arkadia for Visual Studio Code

[English](README.md) | **日本語**

- `.ark` ファイルのシンタックスハイライト
- `ark lsp` による診断（型エラー）、ホバーでの型表示、定義へジャンプ、ドキュメントのフォーマット（`ark fmt` と同じ整形）

## 使い方

```sh
cd editors/vscode
npm install
# VS Code でこのフォルダを開き F5、または vsce package で .vsix を作成
```

`ark` が `PATH` に無い場合は設定 `arkadia.serverPath` で実行ファイルを指定してください。
