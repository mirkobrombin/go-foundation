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
