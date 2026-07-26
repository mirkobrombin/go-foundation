"use strict";

const assert = require("node:assert/strict");
const Module = require("node:module");
const test = require("node:test");

// The extension only runs inside VS Code, so the editor API is replaced by a
// stub that records what the providers return. The workspace lives in memory.
const files = new Map();

function setFile(name, text) {
  files.set(name, text);
}

class Position {
  constructor(line, character) {
    this.line = line;
    this.character = character;
  }
}

class Range {
  constructor(start, character, endLine, endCharacter) {
    if (typeof start === "number") {
      this.start = new Position(start, character);
      this.end = new Position(endLine, endCharacter);
      return;
    }
    this.start = start;
    this.end = character;
  }
}

class Location {
  constructor(uri, rangeOrPosition) {
    this.uri = uri;
    this.range =
      rangeOrPosition instanceof Range
        ? rangeOrPosition
        : new Range(rangeOrPosition, rangeOrPosition);
  }
}

const uriFor = (name) => ({
  fsPath: `/workspace/${name}`,
  path: `/workspace/${name}`,
  toString: () => `file:///workspace/${name}`,
});

const folder = { uri: { fsPath: "/workspace", toString: () => "file:///workspace" } };

function documentFor(name) {
  const text = files.get(name);
  const lines = text.split("\n");
  return {
    uri: uriFor(name),
    languageId: "go",
    lineCount: lines.length,
    getText: () => text,
    lineAt: (line) => ({
      range: { end: new Position(line, lines[line].length) },
    }),
    offsetAt: (position) => {
      let offset = 0;
      for (let index = 0; index < position.line; index += 1) {
        offset += lines[index].length + 1;
      }
      return offset + position.character;
    },
    positionAt: (offset) => {
      let remaining = offset;
      for (let line = 0; line < lines.length; line += 1) {
        if (remaining <= lines[line].length) {
          return new Position(line, remaining);
        }
        remaining -= lines[line].length + 1;
      }
      return new Position(lines.length - 1, 0);
    },
  };
}

const messages = [];
const executed = [];
const opened = [];
const registered = { commands: new Map(), providers: {} };

const vscodeStub = {
  Position,
  Range,
  Location,
  Hover: class Hover {
    constructor(contents) {
      this.contents = contents;
    }
  },
  CodeLens: class CodeLens {
    constructor(range, command) {
      this.range = range;
      this.command = command;
    }
  },
  Diagnostic: class Diagnostic {},
  DiagnosticSeverity: { Error: 0 },
  EventEmitter: class EventEmitter {
    constructor() {
      this.event = () => ({ dispose() {} });
    }
    fire() {}
  },
  RelativePattern: class RelativePattern {},
  Uri: { file: (path) => uriFor(path.replace("/workspace/", "")) },
  commands: {
    registerCommand(name, handler) {
      registered.commands.set(name, handler);
      return { dispose() {} };
    },
    executeCommand: async (name, ...args) => {
      executed.push({ name, args });
      return undefined;
    },
  },
  languages: {
    createDiagnosticCollection: () => ({
      forEach() {},
      set() {},
      delete() {},
      dispose() {},
    }),
    registerCodeLensProvider(_selector, provider) {
      registered.providers.lens = provider;
      return { dispose() {} };
    },
    registerHoverProvider(_selector, provider) {
      registered.providers.hover = provider;
      return { dispose() {} };
    },
    registerDefinitionProvider(_selector, provider) {
      registered.providers.definition = provider;
      return { dispose() {} };
    },
    registerImplementationProvider(_selector, provider) {
      registered.providers.implementation = provider;
      return { dispose() {} };
    },
    registerReferenceProvider(_selector, provider) {
      registered.providers.references = provider;
      return { dispose() {} };
    },
  },
  window: {
    activeTextEditor: undefined,
    showInformationMessage: (message) => messages.push(message),
    showWarningMessage: (message) => messages.push(message),
    showErrorMessage: (message) => messages.push(message),
    showTextDocument: async (target) => {
      opened.push(target.fsPath ?? target.uri?.fsPath);
      return undefined;
    },
  },
  workspace: {
    isTrusted: true,
    workspaceFolders: [folder],
    getWorkspaceFolder: () => folder,
    getConfiguration: () => ({ get: (_key, fallback) => fallback }),
    onDidSaveTextDocument: () => ({ dispose() {} }),
    openTextDocument: async (uri) =>
      documentFor(uri.fsPath.replace("/workspace/", "")),
    findFiles: async () => [...files.keys()].map(uriFor),
    fs: {
      stat: async (uri) => ({
        size: Buffer.byteLength(files.get(uri.fsPath.replace("/workspace/", ""))),
      }),
      readFile: async (uri) =>
        Buffer.from(files.get(uri.fsPath.replace("/workspace/", "")), "utf8"),
    },
  },
};

const load = Module._load;
Module._load = function (request, parent, isMain) {
  if (request === "vscode") {
    return vscodeStub;
  }
  return load.apply(this, [request, parent, isMain]);
};

const extension = require("./extension");
extension.activate({ subscriptions: [] });

setFile(
  "contracts.go",
  `package app

type UserStore interface {
	Find(int) (User, bool)
}

type Clock interface {
	Now() int64
}
`,
);
setFile(
  "clock.go",
  `package app

type SystemClock struct {
	contracts.Implements[Clock]
}
`,
);
setFile(
  "memory.go",
  `package app

type MemoryUserStore struct {
	contracts.Implements[UserStore]
}
`,
);
setFile(
  "cached.go",
  `package app

var _ = contracts.Assert[UserStore]((*CachedUserStore)(nil))

type CachedUserStore struct{}
`,
);
setFile(
  "wiring.go",
  `package app

type GetUser struct {
	_     struct{}  \`method:"GET" path:"/users/{id:int}"\`
	Users UserStore \`inject:"users"\`
}

func build() {
	application.Provide("users", NewMemoryUserStore())
	application.RegisterHTTP(&GetUser{})
}
`,
);

function positionOf(name, needle, extra = 0) {
  const document = documentFor(name);
  return document.positionAt(files.get(name).indexOf(needle) + extra);
}

test("goes from a contract marker to the interface declaration", async () => {
  const document = documentFor("memory.go");
  const found = await registered.providers.definition.provideDefinition(
    document,
    positionOf("memory.go", "contracts.Implements[UserStore]", 5),
  );

  assert.equal(found.length, 1);
  assert.equal(found[0].uri.fsPath, "/workspace/contracts.go");
  assert.equal(found[0].range.start.line, 2);
});

test("goes from the interface declaration to every implementation", async () => {
  const document = documentFor("contracts.go");
  const found = await registered.providers.implementation.provideImplementation(
    document,
    positionOf("contracts.go", "UserStore interface", 1),
  );

  assert.deepEqual(
    found.map((location) => location.uri.fsPath).sort(),
    ["/workspace/cached.go", "/workspace/memory.go"],
  );
});

test("goes from an implementing type name back to its contract", async () => {
  const document = documentFor("memory.go");
  const found = await registered.providers.implementation.provideImplementation(
    document,
    positionOf("memory.go", "MemoryUserStore struct", 1),
  );

  assert.equal(found.length, 1);
  assert.equal(found[0].uri.fsPath, "/workspace/contracts.go");
});

test("links an injected field to the provider and back", async () => {
  const injected = await registered.providers.definition.provideDefinition(
    documentFor("wiring.go"),
    positionOf("wiring.go", 'inject:"users"', 9),
  );
  assert.equal(injected.length, 1);
  assert.equal(injected[0].uri.fsPath, "/workspace/wiring.go");

  const fromProvider = await registered.providers.references.provideReferences(
    documentFor("wiring.go"),
    positionOf("wiring.go", '"users", NewMemoryUserStore', 1),
    {},
  );
  assert.equal(fromProvider.length, 2);
});

test("offers navigating lenses instead of plain labels", async () => {
  const lenses = await registered.providers.lens.provideCodeLenses(
    documentFor("memory.go"),
  );

  assert.equal(lenses.length, 1);
  assert.equal(lenses[0].command.title, "Foundation contract UserStore");
  assert.equal(lenses[0].command.command, "foundation.revealTargets");
  assert.equal(lenses[0].command.arguments[2], "definition");
});

test("opens the full implementation list instead of jumping to one", async () => {
  const reveal = registered.commands.get("foundation.revealTargets");
  const position = positionOf("contracts.go", "UserStore interface", 1);

  executed.length = 0;
  opened.length = 0;
  await reveal(uriFor("contracts.go"), position, "references");

  const peek = executed.find((call) => call.name === "editor.action.showReferences");
  assert.ok(peek, "the reference peek must be used for implementation lists");
  assert.deepEqual(
    peek.args[2].map((location) => location.uri.fsPath).sort(),
    ["/workspace/cached.go", "/workspace/memory.go"],
  );
  assert.deepEqual(opened, ["/workspace/contracts.go"]);
});

test("keeps showing the list when a contract has a single implementation", async () => {
  const reveal = registered.commands.get("foundation.revealTargets");

  executed.length = 0;
  await reveal(
    uriFor("contracts.go"),
    positionOf("contracts.go", "Clock interface", 1),
    "references",
  );

  const peek = executed.find((call) => call.name === "editor.action.showReferences");
  assert.ok(peek);
  assert.deepEqual(
    peek.args[2].map((location) => location.uri.fsPath),
    ["/workspace/clock.go"],
  );
});

test("counts implementations in the interface lens", async () => {
  const document = documentFor("contracts.go");
  await registered.providers.lens.provideCodeLenses(document);
  await new Promise((resolve) => setTimeout(resolve, 20));
  const lenses = await registered.providers.lens.provideCodeLenses(document);

  assert.deepEqual(
    lenses.map((lens) => lens.command.title).sort(),
    ["Foundation: 1 implementation", "Foundation: 2 implementations"],
  );
  assert.equal(lenses[0].command.arguments[2], "references");
});

test("resolves a route lens to the registration site", async () => {
  const reveal = registered.commands.get("foundation.revealTargets");
  assert.ok(reveal);

  const routeTag = positionOf("wiring.go", 'method:"GET"', 1);
  messages.length = 0;
  await reveal(uriFor("wiring.go"), routeTag, "registrations");
  assert.deepEqual(messages, []);

  const lenses = await registered.providers.lens.provideCodeLenses(
    documentFor("wiring.go"),
  );
  const route = lenses.find((lens) =>
    lens.command.title.startsWith("Foundation route"),
  );
  assert.equal(route.command.arguments[2], "registrations");
});

test("reports missing registrations instead of staying silent", async () => {
  const reveal = registered.commands.get("foundation.revealTargets");
  setFile(
    "orphan.go",
    `package app

type Unregistered struct {
	_ struct{} \`method:"GET" path:"/orphan"\`
}
`,
  );

  messages.length = 0;
  await reveal(uriFor("orphan.go"), positionOf("orphan.go", 'method:"GET"', 1), "registrations");
  assert.deepEqual(messages, [
    "No registration found. Run Foundation generate, or register the type explicitly.",
  ]);
  files.delete("orphan.go");
});
