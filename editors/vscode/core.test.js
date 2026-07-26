"use strict";

const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");
const core = require("./core");

test("parses analyzer diagnostics", () => {
  const diagnostics = core.parseAnalyzerOutput(
    "handlers.go:12:4: route parameter id has no path field\nnoise",
    "/workspace",
  );

  assert.deepEqual(diagnostics, [
    {
      file: path.resolve("/workspace", "handlers.go"),
      line: 11,
      column: 3,
      message: "route parameter id has no path field",
    },
  ]);
});

test("limits analyzer diagnostics", () => {
  const diagnostics = core.parseAnalyzerOutput(
    "a.go:1:1: first\nb.go:2:2: second",
    "/workspace",
    1,
  );
  assert.equal(diagnostics.length, 1);
  assert.equal(diagnostics[0].message, "first");
});

test("converts offsets to line and column", () => {
  assert.deepEqual(core.positionAt("first\nsecond", 8), {
    line: 1,
    column: 2,
  });
});

test("validates package patterns", () => {
  assert.deepEqual(core.validatePackagePatterns(["./...", "example.com/mod"]), [
    "./...",
    "example.com/mod",
  ]);
  assert.throws(() => core.validatePackagePatterns(["-cpuprofile=/tmp/out"]));
  assert.throws(() => core.validatePackagePatterns(["./...\n-bad"]));
});

test("scans Foundation metadata", () => {
  const metadata = core.scanMetadata(`
type Endpoint struct {
  _ struct{} \`path:"/users/{id:int}" method:"GET"\`
  Users Store \`inject:"users"\`
  contracts.Implements[Handler]
}

// contracts.Assert[CommentOnly]
const ignored = \`inject:"ignored"\`
`);

  assert.deepEqual(
    metadata.map((item) => [item.kind, item.label]),
    [
      ["route", "Foundation route GET /users/{id:int}"],
      ["dependency", "Foundation dependency users"],
      ["contract", "Foundation contract Handler"],
    ],
  );
});

test("ignores metadata in comments and non-struct strings", () => {
  const metadata = core.scanMetadata(`
// type Fake struct { Value string \`inject:"comment"\` }
const quoted = "contracts.Assert[Quoted]"
const raw = \`method:"POST" path:"/fake"\`

type Real struct {
  _ struct{} \`keys:"id" action:"read"\`
}
`);

  assert.deepEqual(
    metadata.map((item) => [item.kind, item.label]),
    [["action", "Foundation action read (id)"]],
  );
});

test("finds type declarations, including grouped ones", () => {
  const source = `
type UserStore interface {
  Find(int) (User, bool)
}

type MemoryUserStore struct {
  contracts.Implements[UserStore]
}

type (
  Reader interface { Read() error }
  Buffer struct { data []byte }
)

type Registry[T any] struct { items []T }
`;
  assert.deepEqual(
    core.typeDeclarations(source).map((item) => [item.name, item.kind]),
    [
      ["UserStore", "interface"],
      ["MemoryUserStore", "struct"],
      ["Reader", "interface"],
      ["Buffer", "struct"],
      ["Registry", "struct"],
    ],
  );

  const onName = source.indexOf("UserStore interface") + 2;
  assert.deepEqual(core.typeDeclarationAt(source, onName), {
    name: "UserStore",
    kind: "interface",
    index: source.indexOf("UserStore interface"),
  });
  assert.equal(core.typeDeclarationAt(source, source.indexOf("Find(int)")), undefined);
});

test("links a contract marker to its implementing type", () => {
  const source = `
type MemoryUserStore struct {
  contracts.Implements[UserStore]
}

var _ = contracts.Assert[UserStore]((*CachedUserStore)(nil))

// contracts.Implements[Commented]
`;
  const markers = core.contractMarkers(source);
  assert.deepEqual(
    markers.map((marker) => [marker.contract, marker.kind, marker.typeName]),
    [
      ["UserStore", "implements", "MemoryUserStore"],
      ["UserStore", "assert", "CachedUserStore"],
    ],
  );

  const inside = source.indexOf("contracts.Implements[UserStore]") + 5;
  assert.equal(core.contractAt(source, inside), "UserStore");
  assert.equal(core.contractsOfType(source, "MemoryUserStore")[0], "UserStore");
  assert.equal(core.contractOffsets(source, "UserStore").length, 2);
  assert.equal(core.contractOffsets(source, "Missing").length, 0);
});

test("navigates from a contract name to its declaration", () => {
  const source = `
type UserStore interface {
  Find(int) (User, bool)
}
`;
  assert.deepEqual(core.declarationOffsets(source, "UserStore"), [
    source.indexOf("UserStore"),
  ]);
  assert.deepEqual(core.declarationOffsets(source, "Absent"), []);
});

test("finds registration sites of a declared type", () => {
  const source = `
type GetUser struct{}

func register() {
  application.RegisterHTTP(&GetUser{})
  handlers := []any{GetUser{}}
  // application.RegisterHTTP(&GetUser{})
}
`;
  assert.equal(core.typeUsageOffsets(source, "GetUser").length, 2);
  assert.equal(core.typeUsageOffsets(source, "Other").length, 0);
});

test("reads the dependency key from a provider call", () => {
  const source = `app.New().Provide("users", store)`;
  assert.equal(core.providerKeyAt(source, source.indexOf("users") + 1), "users");
  assert.equal(core.providerKeyAt(source, source.indexOf("store")), undefined);
});

test("links injected dependencies to providers", () => {
  const source = `
type Endpoint struct {
  Users Store \`inject:"users"\`
}

func build() {
  app.New().Provide("users", store)
  app.New().Provide(\`users\`, otherStore)
  // app.New().Provide("users", ignored)
}
`;
  const dependency = source.indexOf('inject:"users"') + 9;
  assert.equal(core.dependencyAt(source, dependency), "users");
  assert.equal(core.providerOffsets(source, "users").length, 2);
  assert.equal(core.dependencyOffsets(source, "users").length, 1);
});
