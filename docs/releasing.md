# リリース手順

1. バージョン番号を更新する（3 か所）
   - `compiler/src/Main.ark` の `version`（`ark version` が表示する値。リリースのタグと一致する必要がある）
   - `compiler/ark.toml` の `version`
   - `editors/vscode/package.json` の `version`
2. `CHANGELOG.md` の `[Unreleased]` の内容を新しいバージョンの節 `## [X.Y.Z] - YYYY-MM-DD` に移し、末尾のリンクを更新する
3. PR を作って CI が通ったら main にマージする
4. main の最新コミットにタグを付けて push する

   ```sh
   git switch main && git pull
   git tag -a vX.Y.Z -m "Arkadia vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. Release ワークフロー（`.github/workflows/release.yml`）が検査・テストの後、`just dist` のアーカイブ
   （`arkadia-X.Y.Z-{linux,darwin}-{amd64,arm64}.tar.gz` と `SHA256SUMS`）と CHANGELOG の該当節を載せた
   GitHub Release を作成する。タグと `ark version` が一致しない場合や CHANGELOG に節が無い場合は失敗する

アーカイブはローカルでも `just dist` で `dist/` に作れます。
