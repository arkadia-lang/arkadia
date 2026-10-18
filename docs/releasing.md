# Release procedure

**English** | [日本語](releasing.ja.md)

1. Update the version number (3 places)
   - `version` in `compiler/src/Main.ark` (the value `ark version` prints; it must match the release tag)
   - `version` in `compiler/ark.toml`
   - `version` in `editors/vscode/package.json`
2. Move the contents under `## [Unreleased]` in `CHANGELOG.md` into a new version section `## [X.Y.Z] - YYYY-MM-DD` (the release date) created right after it,
   and leave `[Unreleased]` empty. Update the links at the end as well. Do the same in `CHANGELOG.ja.md`

   ```markdown
   [Unreleased]: https://github.com/arkadia-lang/arkadia/compare/vX.Y.Z...HEAD
   [X.Y.Z]: https://github.com/arkadia-lang/arkadia/releases/tag/vX.Y.Z
   ```

   (for versions after 0.1.0, use `[X.Y.Z]: https://github.com/arkadia-lang/arkadia/compare/v<previous version>...vX.Y.Z` and keep the existing lines)
3. Open a PR, and merge it into main once CI passes
4. Tag the latest commit on main and push the tag

   ```sh
   git switch main && git pull
   git tag -a vX.Y.Z -m "Arkadia vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. After checks and tests, the Release workflow (`.github/workflows/release.yml`) creates a GitHub Release with the `just dist` archives
   (`arkadia-X.Y.Z-{linux,darwin}-{amd64,arm64}.tar.gz` and `SHA256SUMS`) and the corresponding section of `CHANGELOG.md`.
   It fails if the tag does not match `ark version` or if the CHANGELOG has no section for the version

The archives can also be built locally into `dist/` with `just dist`.
