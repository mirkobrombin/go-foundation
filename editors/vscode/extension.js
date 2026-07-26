"use strict";

const childProcess = require("child_process");
const vscode = require("vscode");
const core = require("./core");

const diagnosticSource = "foundation";
const maximumGoFiles = 5000;
const maximumGoFileSize = 2 * 1024 * 1024;
const maximumWorkspaceScanSize = 32 * 1024 * 1024;
const maximumLocations = 10000;
const maximumDiagnostics = 2000;
const commandTimeout = 2 * 60 * 1000;
const indexLifetime = 15 * 1000;

// Contract index per workspace folder. It only exists to decide which
// interfaces deserve a lens, so a stale entry costs a redundant lens, never a
// wrong jump: navigation always rescans.
const contractIndex = new Map();

function activate(context) {
  const diagnostics = vscode.languages.createDiagnosticCollection(diagnosticSource);
  const lenses = new FoundationCodeLensProvider();
  let timer;

  context.subscriptions.push(
    diagnostics,
    vscode.commands.registerCommand("foundation.checkWorkspace", () =>
      checkWorkspace(vscode.window.activeTextEditor?.document, diagnostics, true),
    ),
    vscode.commands.registerCommand("foundation.generate", () =>
      generateWorkspace(vscode.window.activeTextEditor?.document),
    ),
    vscode.commands.registerCommand("foundation.revealTargets", revealTargets),
    vscode.workspace.onDidSaveTextDocument((document) => {
      if (document.languageId !== "go") {
        return;
      }
      contractIndex.clear();
      lenses.refresh();
      if (!vscode.workspace.isTrusted) {
        return;
      }
      const enabled = vscode.workspace
        .getConfiguration("foundation", document.uri)
        .get("checkOnSave", true);
      if (!enabled) {
        return;
      }
      clearTimeout(timer);
      timer = setTimeout(() => checkWorkspace(document, diagnostics), 250);
    }),
    vscode.languages.registerCodeLensProvider({ language: "go", scheme: "file" }, lenses),
    vscode.languages.registerHoverProvider(
      { language: "go", scheme: "file" },
      new FoundationHoverProvider(),
    ),
    vscode.languages.registerDefinitionProvider(
      { language: "go", scheme: "file" },
      new FoundationNavigationProvider("definition"),
    ),
    vscode.languages.registerImplementationProvider(
      { language: "go", scheme: "file" },
      new FoundationNavigationProvider("implementation"),
    ),
    vscode.languages.registerReferenceProvider(
      { language: "go", scheme: "file" },
      new FoundationNavigationProvider("references"),
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
    contractIndex.clear();
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

// targets resolves what the position points at into workspace locations. The
// same resolution backs go to definition, go to implementation, find
// references, and the code lenses, so every entry point agrees.
async function targets(document, position, token, mode) {
  const text = documentTextWithinLimit(document);
  if (text === undefined) {
    return [];
  }
  const folder = vscode.workspace.getWorkspaceFolder(document.uri);
  if (!folder) {
    return [];
  }
  const offset = document.offsetAt(position);

  if (mode === "registrations") {
    const owner = core.enclosingTypeAt(text, offset);
    return owner
      ? collect(folder, token, (source) => core.typeUsageOffsets(source, owner))
      : [];
  }

  const dependency =
    core.dependencyAt(text, offset) ?? core.providerKeyAt(text, offset);
  if (dependency) {
    return collect(folder, token, (source) => {
      const offsets = core.providerOffsets(source, dependency);
      if (mode !== "definition") {
        offsets.push(...core.dependencyOffsets(source, dependency));
      }
      return offsets;
    });
  }

  const contract = core.contractAt(text, offset);
  if (contract) {
    return collect(folder, token, (source) =>
      mode === "references"
        ? core.contractOffsets(source, contract)
        : core.declarationOffsets(source, contract),
    );
  }

  const declaration = core.typeDeclarationAt(text, offset);
  if (declaration?.kind === "interface") {
    return collect(folder, token, (source) =>
      core.contractOffsets(source, declaration.name),
    );
  }
  if (declaration?.kind === "struct") {
    const contracts = core.contractsOfType(text, declaration.name);
    if (contracts.length > 0) {
      return collect(folder, token, (source) =>
        contracts.flatMap((name) => core.declarationOffsets(source, name)),
      );
    }
  }
  return [];
}

async function collect(folder, token, extract) {
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
    for (const offset of extract(text)) {
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

async function revealTargets(uri, position, mode) {
  const document = await vscode.workspace.openTextDocument(uri);
  const found = await targets(document, position, undefined, mode);

  if (found.length === 0) {
    vscode.window.showInformationMessage(emptyMessage(mode));
    return;
  }
  if (found.length === 1) {
    const target = found[0];
    await vscode.window.showTextDocument(target.uri, {
      selection: new vscode.Range(target.range.start, target.range.start),
    });
    return;
  }
  await vscode.window.showTextDocument(document, { preserveFocus: false });
  await vscode.commands.executeCommand(
    "editor.action.peekLocations",
    uri,
    position,
    found,
    "peek",
  );
}

function emptyMessage(mode) {
  if (mode === "registrations") {
    return "No registration found. Run Foundation generate, or register the type explicitly.";
  }
  if (mode === "references") {
    return "No Foundation implementations found.";
  }
  return "No Foundation declaration found for this position.";
}

async function contractsWithImplementations(folder, token) {
  const key = folder.uri.toString();
  const cached = contractIndex.get(key);
  if (cached?.pending || (cached && Date.now() - cached.at <= indexLifetime)) {
    return cached.names;
  }

  const previous = cached?.names ?? new Set();
  contractIndex.set(key, { at: cached?.at ?? 0, names: previous, pending: true });

  const names = new Set();
  try {
    const files = await vscode.workspace.findFiles(
      new vscode.RelativePattern(folder, "**/*.go"),
      "**/{vendor,.git}/**",
      maximumGoFiles,
    );
    let scannedBytes = 0;
    for (const uri of files) {
      if (token?.isCancellationRequested) {
        break;
      }
      const stat = await vscode.workspace.fs.stat(uri);
      if (
        stat.size > maximumGoFileSize ||
        scannedBytes + stat.size > maximumWorkspaceScanSize
      ) {
        continue;
      }
      scannedBytes += stat.size;
      const bytes = await vscode.workspace.fs.readFile(uri);
      const text = Buffer.from(bytes).toString("utf8");
      for (const marker of core.contractMarkers(text)) {
        names.add(marker.contract);
      }
    }
  } catch {
    contractIndex.set(key, { at: cached?.at ?? 0, names: previous, pending: false });
    return previous;
  }

  contractIndex.set(key, { at: Date.now(), names, pending: false });
  return names;
}

class FoundationCodeLensProvider {
  constructor() {
    this.changed = new vscode.EventEmitter();
    this.onDidChangeCodeLenses = this.changed.event;
  }

  refresh() {
    this.changed.fire();
  }

  provideCodeLenses(document, token) {
    const text = documentTextWithinLimit(document);
    if (text === undefined) {
      return [];
    }

    const lenses = core.scanMetadata(text).map((item) => {
      const position = document.positionAt(item.index);
      return new vscode.CodeLens(new vscode.Range(position, position), {
        command: "foundation.revealTargets",
        title: item.label,
        arguments: [document.uri, position, lensMode(item.kind)],
      });
    });

    const folder = vscode.workspace.getWorkspaceFolder(document.uri);
    const interfaces = core
      .typeDeclarations(text)
      .filter((declaration) => declaration.kind === "interface");
    if (!folder || interfaces.length === 0) {
      return lenses;
    }

    const known = contractIndex.get(folder.uri.toString());
    const stale = !known || Date.now() - known.at > indexLifetime;
    if (stale && !known?.pending) {
      contractsWithImplementations(folder, token).then((names) => {
        if (names.size > 0) {
          this.refresh();
        }
      });
    }
    if (!known) {
      return lenses;
    }

    for (const declaration of interfaces) {
      if (!known.names.has(declaration.name)) {
        continue;
      }
      const position = document.positionAt(declaration.index);
      lenses.push(
        new vscode.CodeLens(new vscode.Range(position, position), {
          command: "foundation.revealTargets",
          title: "Foundation implementations",
          arguments: [document.uri, position, "references"],
        }),
      );
    }
    return lenses;
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

class FoundationNavigationProvider {
  constructor(mode) {
    this.mode = mode;
  }

  async provideDefinition(document, position, token) {
    const found = await targets(document, position, token, this.mode);
    return found.length > 0 ? found : undefined;
  }

  async provideImplementation(document, position, token) {
    const found = await targets(document, position, token, this.mode);
    return found.length > 0 ? found : undefined;
  }

  async provideReferences(document, position, context, token) {
    return targets(document, position, token, this.mode);
  }
}

function lensMode(kind) {
  if (kind === "contract") {
    return "definition";
  }
  if (kind === "dependency") {
    return "definition";
  }
  return "registrations";
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
