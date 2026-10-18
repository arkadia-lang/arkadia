# リリース手順

[English](releasing.md) | **日本語**

1. バージョン番号を更新する（3 か所）
   - `compiler/src/Main.ark` の `version`（`ark version` が表示する値。リリースのタグと一致する必要がある）
   - `compiler/ark.toml` の `version`
   - `editors/vscode/package.json` の `version`
2. `CHANGELOG.md` の `## [Unreleased]` の下の内容を、その直後に作る新しいバージョンの節 `## [X.Y.Z] - YYYY-MM-DD`（リリース日）に移し、
   `[Unreleased]` は空にする。末尾のリンクも更新する。`CHANGELOG.ja.md` も同様に更新する

   ```markdown
   [Unreleased]: https://github.com/arkadia-lang/arkadia/compare/vX.Y.Z...HEAD
   [X.Y.Z]: https://github.com/arkadia-lang/arkadia/releases/tag/vX.Y.Z
   ```

   （0.1.0 より後のバージョンは `[X.Y.Z]: https://github.com/arkadia-lang/arkadia/compare/v<前のバージョン>...vX.Y.Z` にし、既存の行は残す）
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
