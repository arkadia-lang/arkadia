# Arkadia for Visual Studio Code

**English** | [日本語](README.ja.md)

- Syntax highlighting for `.ark` files
- Diagnostics (type errors), types on hover, go to definition and document formatting (the same formatting as `ark fmt`) via `ark lsp`

## Usage

```sh
cd editors/vscode
npm install
# open this folder in VS Code and press F5, or build a .vsix with vsce package
```

If `ark` is not on your `PATH`, set the executable with the `arkadia.serverPath` setting.
