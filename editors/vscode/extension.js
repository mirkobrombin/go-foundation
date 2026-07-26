"use strict";

const childProcess = require("child_process");
const path = require("path");
const vscode = require("vscode");
const core = require("./core");

const diagnosticSource = "foundation";
const maximumGoFiles = 5000;
const maximumGoFileSize = 2 * 1024 * 1024;
const maximumWorkspaceScanSize = 32 * 1024 * 1024;
const maximumLocations = 10000;
const maximumDiagnostics = 2000;
const commandTimeout = 2 * 60 * 1000;

function activate(context) {
  const diagnostics = vscode.languages.createDiagnosticCollection(diagnosticSource);
  context.subscriptions.push(diagnostics);

  const runCheck = (document) => checkWorkspace(document, diagnostics);
  let timer;

  context.subscriptions.push(
    vscode.commands.registerCommand("foundation.checkWorkspace", () =>
      checkWorkspace(vscode.window.activeTextEditor?.document, diagnostics, true),
    ),
    vscode.commands.registerCommand("foundation.generate", () =>
      generateWorkspace(vscode.window.activeTextEditor?.document),
    ),
    vscode.commands.registerCommand("foundation.showMetadata", (label) =>
      vscode.window.showInformationMessage(label),
    ),
    vscode.workspace.onDidSaveTextDocument((document) => {
      if (!vscode.workspace.isTrusted) {
        return;
      }
      const enabled = vscode.workspace
        .getConfiguration("foundation", document.uri)
        .get("checkOnSave", true);
      if (!enabled || document.languageId !== "go") {
        return;
      }
      clearTimeout(timer);
      timer = setTimeout(() => runCheck(document), 250);
    }),
    vscode.languages.registerCodeLensProvider(
      { language: "go", scheme: "file" },
      new FoundationCodeLensProvider(),
    ),
    vscode.languages.registerHoverProvider(
      { language: "go", scheme: "file" },
      new FoundationHoverProvider(),
    ),
    vscode.languages.registerDefinitionProvider(
      { language: "go", scheme: "file" },
      new FoundationDependencyProvider(false),
    ),
    vscode.languages.registerReferenceProvider(
      { language: "go", scheme: "file" },
      new FoundationDependencyProvider(true),
    ),
  );
}

async function checkWorkspace(document, collection, notify = false) {
  if (!ensureTrustedWorkspace()) {
    return;
  }
  const folder = workspaceFolder(document);
  if (!folder) {
    if (notify) {
      vscode.window.showWarningMessage("Open a Foundation workspace first.");
    }
    return;
  }

  const configuration = vscode.workspace.getConfiguration(
    "foundation",
    document?.uri,
  );
  const executable = configuration.get("executable", "foundation");

  try {
    const patterns = core.validatePackagePatterns(
      configuration.get("checkPatterns", ["./..."]),
    );
    const output = await execute(executable, ["check", ...patterns], folder.uri.fsPath);
    applyDiagnostics(collection, folder, output);
    if (notify) {
      vscode.window.showInformationMessage("Foundation check passed.");
    }
  } catch (error) {
    const output = `${error.stdout || ""}\n${error.stderr || ""}`;
    applyDiagnostics(collection, folder, output);
    if (
      core.parseAnalyzerOutput(
        output,
        folder.uri.fsPath,
        maximumDiagnostics,
      ).length === 0
    ) {
      vscode.window.showErrorMessage(`Foundation check failed: ${error.message}`);
    }
  }
}

async function generateWorkspace(document) {
  if (!ensureTrustedWorkspace()) {
    return;
  }
  const folder = workspaceFolder(document);
  if (!folder) {
    vscode.window.showWarningMessage("Open a Foundation workspace first.");
    return;
  }
  const configuration = vscode.workspace.getConfiguration(
    "foundation",
    document?.uri,
  );
  const executable = configuration.get("executable", "foundation");

  try {
    const patterns = core.validatePackagePatterns(
      configuration.get("checkPatterns", ["./..."]),
    );
    const output = await execute(
      executable,
      ["generate", ...patterns],
      folder.uri.fsPath,
    );
    vscode.window.showInformationMessage(
      output.trim() || "Foundation generation completed.",
    );
  } catch (error) {
    vscode.window.showErrorMessage(
      `Foundation generation failed: ${error.stderr || error.message}`,
    );
  }
}

function applyDiagnostics(collection, folder, output) {
  const grouped = new Map();
  const parsed = core.parseAnalyzerOutput(
    output,
    folder.uri.fsPath,
    maximumDiagnostics,
  );
  for (const item of parsed) {
    const uri = vscode.Uri.file(item.file);
    const diagnostic = new vscode.Diagnostic(
      new vscode.Range(item.line, item.column, item.line, item.column + 1),
      item.message,
      vscode.DiagnosticSeverity.Error,
    );
    diagnostic.source = diagnosticSource;
    const key = uri.toString();
    const group = grouped.get(key) || { uri, diagnostics: [] };
    group.diagnostics.push(diagnostic);
    grouped.set(key, group);
  }

  const existing = [];
  collection.forEach((uri) => existing.push(uri));
  for (const uri of existing) {
    if (
      vscode.workspace.getWorkspaceFolder(uri)?.uri.toString() ===
      folder.uri.toString()
    ) {
      collection.delete(uri);
    }
  }
  for (const group of grouped.values()) {
    collection.set(group.uri, group.diagnostics);
  }
  if (parsed.length === maximumDiagnostics) {
    vscode.window.showWarningMessage(
      `Foundation diagnostics were limited to ${maximumDiagnostics} results.`,
    );
  }
}

class FoundationCodeLensProvider {
  provideCodeLenses(document) {
    const text = documentTextWithinLimit(document);
    if (text === undefined) {
      return [];
    }
    return core.scanMetadata(text).map((item) => {
      const position = document.positionAt(item.index);
      return new vscode.CodeLens(new vscode.Range(position, position), {
        command: "foundation.showMetadata",
        title: item.label,
        arguments: [item.label],
      });
    });
  }
}

class FoundationHoverProvider {
  provideHover(document, position) {
    const text = documentTextWithinLimit(document);
    if (text === undefined) {
      return undefined;
    }
    const offset = document.offsetAt(position);
    const item = core
      .scanMetadata(text)
      .find(
        (candidate) =>
          offset >= candidate.index &&
          offset <= candidate.index + candidate.length,
      );
    return item ? new vscode.Hover(item.label) : undefined;
  }
}

class FoundationDependencyProvider {
  constructor(includeReferences) {
    this.includeReferences = includeReferences;
  }

  async provideDefinition(document, position, token) {
    const locations = await this.locations(document, position, token);
    return locations.length > 0 ? locations : undefined;
  }

  async provideReferences(document, position, context, token) {
    return this.locations(document, position, token);
  }

  async locations(document, position, token) {
    const documentText = documentTextWithinLimit(document);
    if (documentText === undefined) {
      return [];
    }
    const key = core.dependencyAt(documentText, document.offsetAt(position));
    if (!key) {
      return [];
    }

    const folder = vscode.workspace.getWorkspaceFolder(document.uri);
    if (!folder) {
      return [];
    }
    const files = await vscode.workspace.findFiles(
      new vscode.RelativePattern(folder, "**/*.go"),
      "**/{vendor,.git}/**",
      maximumGoFiles,
    );
    const locations = [];
    let scannedBytes = 0;
    for (const uri of files) {
      if (token?.isCancellationRequested) {
        return [];
      }
      const stat = await vscode.workspace.fs.stat(uri);
      if (stat.size > maximumGoFileSize) {
        continue;
      }
      if (scannedBytes + stat.size > maximumWorkspaceScanSize) {
        break;
      }
      const bytes = await vscode.workspace.fs.readFile(uri);
      if (
        bytes.byteLength > maximumGoFileSize ||
        scannedBytes + bytes.byteLength > maximumWorkspaceScanSize
      ) {
        continue;
      }
      scannedBytes += bytes.byteLength;
      const text = Buffer.from(bytes).toString("utf8");
      const offsets = core.providerOffsets(text, key);
      if (this.includeReferences) {
        offsets.push(...core.dependencyOffsets(text, key));
      }
      for (const offset of offsets) {
        const position = core.positionAt(text, offset);
        locations.push(
          new vscode.Location(
            uri,
            new vscode.Position(position.line, position.column),
          ),
        );
        if (locations.length >= maximumLocations) {
          return locations;
        }
      }
    }
    return locations;
  }
}

function documentTextWithinLimit(document) {
  if (document.lineCount === 0) {
    return "";
  }
  const lastLine = document.lineAt(document.lineCount - 1);
  if (document.offsetAt(lastLine.range.end) > maximumGoFileSize) {
    return undefined;
  }
  const text = document.getText();
  return Buffer.byteLength(text, "utf8") <= maximumGoFileSize ? text : undefined;
}

function workspaceFolder(document) {
  if (document) {
    return vscode.workspace.getWorkspaceFolder(document.uri);
  }
  return vscode.workspace.workspaceFolders?.[0];
}

function ensureTrustedWorkspace() {
  if (vscode.workspace.isTrusted) {
    return true;
  }
  vscode.window.showWarningMessage(
    "Foundation commands are disabled in Restricted Mode.",
  );
  return false;
}

function execute(command, args, cwd) {
  return new Promise((resolve, reject) => {
    childProcess.execFile(
      command,
      args,
      {
        cwd,
        maxBuffer: 10 * 1024 * 1024,
        timeout: commandTimeout,
        windowsHide: true,
      },
      (error, stdout, stderr) => {
        if (error) {
          error.stdout = stdout;
          error.stderr = stderr;
          reject(error);
          return;
        }
        resolve(`${stdout}\n${stderr}`);
      },
    );
  });
}

function deactivate() {}

module.exports = { activate, deactivate };
