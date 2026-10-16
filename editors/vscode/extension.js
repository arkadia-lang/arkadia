// Starts `ark lsp` as the language server for .ark files.
const vscode = require("vscode");
const { LanguageClient } = require("vscode-languageclient/node");

let client;

function activate(context) {
  const command = vscode.workspace.getConfiguration("arkadia").get("serverPath") || "ark";
  const server = { command, args: ["lsp"] };
  client = new LanguageClient(
    "arkadia",
    "Arkadia Language Server",
    { run: server, debug: server },
    { documentSelector: [{ scheme: "file", language: "arkadia" }] }
  );
  client.start();
  context.subscriptions.push({ dispose: () => client && client.stop() });
}

function deactivate() {
  return client ? client.stop() : undefined;
}

module.exports = { activate, deactivate };
