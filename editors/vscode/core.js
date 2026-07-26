"use strict";

const path = require("path");

function maskRange(masked, start, end) {
  for (let index = start; index < end; index += 1) {
    if (masked[index] !== "\n" && masked[index] !== "\r") {
      masked[index] = " ";
    }
  }
}

function scanGoSource(text) {
  const masked = Array.from(text);
  const tags = [];
  const strings = [];
  const braces = [];
  let pendingStruct = false;
  let index = 0;

  while (index < text.length) {
    if (text.startsWith("//", index)) {
      const end = text.indexOf("\n", index + 2);
      const stop = end === -1 ? text.length : end;
      maskRange(masked, index, stop);
      index = stop;
      continue;
    }
    if (text.startsWith("/*", index)) {
      const end = text.indexOf("*/", index + 2);
      const stop = end === -1 ? text.length : end + 2;
      maskRange(masked, index, stop);
      index = stop;
      continue;
    }

    const character = text[index];
    if (character === '"' || character === "'") {
      const quote = character;
      let end = index + 1;
      while (end < text.length) {
        if (text[end] === "\\") {
          end += 2;
          continue;
        }
        end += 1;
        if (text[end - 1] === quote) {
          break;
        }
      }
      if (quote === '"') {
        const raw = text.slice(index + 1, Math.max(index + 1, end - 1));
        let value = raw;
        try {
          value = JSON.parse(`"${raw}"`);
        } catch {
          // Invalid source is left to the Go analyzer.
        }
        strings.push({ start: index, end, value });
      }
      maskRange(masked, index, end);
      index = end;
      continue;
    }
    if (character === "`") {
      const closing = text.indexOf("`", index + 1);
      const end = closing === -1 ? text.length : closing + 1;
      if (braces.at(-1) === true) {
        tags.push({
          start: index,
          end,
          content: text.slice(index + 1, closing === -1 ? text.length : closing),
        });
      } else {
        strings.push({
          start: index,
          end,
          value: text.slice(index + 1, closing === -1 ? text.length : closing),
        });
      }
      maskRange(masked, index, end);
      index = end;
      continue;
    }
    if (character === "{") {
      braces.push(pendingStruct);
      pendingStruct = false;
      index += 1;
      continue;
    }
    if (character === "}") {
      braces.pop();
      pendingStruct = false;
      index += 1;
      continue;
    }
    if (/[A-Za-z_]/.test(character)) {
      let end = index + 1;
      while (end < text.length && /[A-Za-z0-9_]/.test(text[end])) {
        end += 1;
      }
      if (text.slice(index, end) === "struct") {
        pendingStruct = true;
      }
      index = end;
      continue;
    }
    index += 1;
  }

  return { masked: masked.join(""), strings, tags };
}

function tagAttributes(tag) {
  const attributes = new Map();
  const pattern = /\b(method|path|action|keys|inject):"([^"]*)"/g;
  for (const match of tag.content.matchAll(pattern)) {
    attributes.set(match[1], {
      index: tag.start + 1 + match.index,
      length: match[0].length,
      value: match[2],
    });
  }
  return attributes;
}

function parseAnalyzerOutput(output, cwd, maximumDiagnostics = Number.MAX_SAFE_INTEGER) {
  const diagnostics = [];
  const pattern = /^(.+\.go):(\d+):(\d+):\s+(.+)$/;

  for (const line of output.split(/\r?\n/)) {
    if (diagnostics.length >= maximumDiagnostics) {
      break;
    }
    const match = pattern.exec(line.trim());
    if (!match) {
      continue;
    }
    diagnostics.push({
      file: path.isAbsolute(match[1]) ? match[1] : path.resolve(cwd, match[1]),
      line: Number(match[2]) - 1,
      column: Number(match[3]) - 1,
      message: match[4],
    });
  }
  return diagnostics;
}

function validatePackagePatterns(patterns) {
  if (!Array.isArray(patterns) || patterns.length === 0 || patterns.length > 100) {
    throw new Error("Foundation package patterns must contain 1 to 100 entries.");
  }
  for (const pattern of patterns) {
    if (
      typeof pattern !== "string" ||
      pattern.length === 0 ||
      pattern.length > 1024 ||
      pattern.startsWith("-") ||
      /[\0\r\n]/.test(pattern)
    ) {
      throw new Error(`Invalid Foundation package pattern: ${String(pattern)}`);
    }
  }
  return patterns;
}

function positionAt(text, offset) {
  const target = Math.max(0, Math.min(offset, text.length));
  let line = 0;
  let lineStart = 0;
  for (let index = 0; index < target; index += 1) {
    if (text.charCodeAt(index) === 10) {
      line += 1;
      lineStart = index + 1;
    }
  }
  return { line, column: target - lineStart };
}

function scanMetadata(text) {
  const metadata = [];
  const source = scanGoSource(text);

  for (const tag of source.tags) {
    const attributes = tagAttributes(tag);
    const method = attributes.get("method");
    const routePath = attributes.get("path");
    const action = attributes.get("action");
    const keys = attributes.get("keys");
    const dependency = attributes.get("inject");

    if (method && routePath) {
      metadata.push({
        kind: "route",
        index: Math.min(method.index, routePath.index),
        length: tag.end - tag.start,
        label: `Foundation route ${method.value} ${routePath.value}`,
      });
    }
    if (action) {
      metadata.push({
        kind: "action",
        index: action.index,
        length: action.length,
        label: keys
          ? `Foundation action ${action.value} (${keys.value})`
          : `Foundation action ${action.value}`,
      });
    }
    if (dependency) {
      metadata.push({
        kind: "dependency",
        index: dependency.index,
        length: dependency.length,
        label: `Foundation dependency ${dependency.value}`,
      });
    }
  }

  const contractPattern = /contracts\.(?:Implements|Assert)\[([^\]]+)\]/g;
  for (const match of source.masked.matchAll(contractPattern)) {
    metadata.push({
      kind: "contract",
      index: match.index,
      length: match[0].length,
      label: `Foundation contract ${match[1]}`,
    });
  }
  return metadata.sort((left, right) => left.index - right.index);
}

function typeDeclarations(text) {
  const masked = scanGoSource(text).masked;
  const declarations = [];
  const single = /\btype\s+([A-Za-z_]\w*)(?:\[[^\]]*\])?\s+(interface|struct)\b/dg;
  for (const match of masked.matchAll(single)) {
    declarations.push({
      name: match[1],
      kind: match[2],
      index: match.indices[1][0],
    });
  }

  // Grouped declarations: type ( Reader interface { ... } ).
  for (const block of masked.matchAll(/\btype\s*\(/g)) {
    const start = block.index + block[0].length;
    let depth = 1;
    let index = start;
    while (index < masked.length && depth > 0) {
      if (masked[index] === "(") {
        depth += 1;
      } else if (masked[index] === ")") {
        depth -= 1;
      }
      index += 1;
    }
    const body = masked.slice(start, Math.max(start, index - 1));
    const entry = /(?:^|\n)[ \t]*([A-Za-z_]\w*)(?:\[[^\]]*\])?\s+(interface|struct)\b/dg;
    for (const match of body.matchAll(entry)) {
      declarations.push({
        name: match[1],
        kind: match[2],
        index: start + match.indices[1][0],
      });
    }
  }

  return declarations.sort((left, right) => left.index - right.index);
}

function typeDeclarationAt(text, offset) {
  for (const declaration of typeDeclarations(text)) {
    if (
      offset >= declaration.index &&
      offset <= declaration.index + declaration.name.length
    ) {
      return declaration;
    }
  }
  return undefined;
}

function enclosingType(masked, offset) {
  const pattern = /\btype\s+([A-Za-z_]\w*)(?:\[[^\]]*\])?\s+struct\b/dg;
  let enclosing;
  for (const match of masked.slice(0, offset).matchAll(pattern)) {
    enclosing = { typeName: match[1], typeIndex: match.indices[1][0] };
  }
  return enclosing;
}

function enclosingTypeAt(text, offset) {
  return enclosingType(scanGoSource(text).masked, offset)?.typeName;
}

function contractMarkers(text) {
  const masked = scanGoSource(text).masked;
  const markers = [];

  for (const match of masked.matchAll(/contracts\.Implements\[([^\]]+)\]/dg)) {
    markers.push({
      contract: match[1].trim(),
      kind: "implements",
      index: match.index,
      length: match[0].length,
      ...enclosingType(masked, match.index),
    });
  }

  const assertion =
    /contracts\.Assert\[([^\]]+)\]\(\s*(?:\(\s*\*\s*([A-Za-z_]\w*)\s*\))?/dg;
  for (const match of masked.matchAll(assertion)) {
    markers.push({
      contract: match[1].trim(),
      kind: "assert",
      index: match.index,
      length: match[0].length,
      typeName: match[2],
      typeIndex: match[2] ? match.indices[2][0] : undefined,
    });
  }

  return markers.sort((left, right) => left.index - right.index);
}

function contractAt(text, offset) {
  for (const marker of contractMarkers(text)) {
    if (offset >= marker.index && offset <= marker.index + marker.length) {
      return marker.contract;
    }
  }
  return undefined;
}

function contractOffsets(text, name) {
  return contractMarkers(text)
    .filter((marker) => marker.contract === name)
    .map((marker) => marker.typeIndex ?? marker.index);
}

function contractsOfType(text, typeName) {
  const contracts = [];
  for (const marker of contractMarkers(text)) {
    if (marker.typeName === typeName && !contracts.includes(marker.contract)) {
      contracts.push(marker.contract);
    }
  }
  return contracts;
}

function declarationOffsets(text, name) {
  const declarations = typeDeclarations(text).filter(
    (declaration) => declaration.name === name,
  );
  const interfaces = declarations.filter(
    (declaration) => declaration.kind === "interface",
  );
  const selected = interfaces.length > 0 ? interfaces : declarations;
  return selected.map((declaration) => declaration.index);
}

function typeUsageOffsets(text, typeName) {
  const masked = scanGoSource(text).masked;
  const offsets = [];
  const pattern = new RegExp(`&?\\b${typeName}\\s*\\{`, "g");
  for (const match of masked.matchAll(pattern)) {
    const prefix = masked.slice(Math.max(0, match.index - 6), match.index);
    if (/\btype\s+$/.test(prefix)) {
      continue;
    }
    offsets.push(match.index);
  }
  return offsets;
}

function providerKeyAt(text, offset) {
  const source = scanGoSource(text);
  for (const literal of source.strings) {
    if (offset < literal.start || offset > literal.end) {
      continue;
    }
    const start = Math.max(0, literal.start - 256);
    if (/\.Provide\(\s*$/.test(source.masked.slice(start, literal.start))) {
      return literal.value;
    }
  }
  return undefined;
}

function dependencyAt(text, offset) {
  for (const tag of scanGoSource(text).tags) {
    const dependency = tagAttributes(tag).get("inject");
    if (
      dependency &&
      offset >= dependency.index &&
      offset <= dependency.index + dependency.length
    ) {
      return dependency.value;
    }
  }
  return undefined;
}

function providerOffsets(text, key) {
  const source = scanGoSource(text);
  const offsets = [];
  for (const literal of source.strings) {
    if (literal.value !== key) {
      continue;
    }
    const start = Math.max(0, literal.start - 256);
    const prefix = source.masked.slice(start, literal.start);
    const match = /\.Provide\(\s*$/.exec(prefix);
    if (match) {
      offsets.push(start + match.index);
    }
  }
  return offsets;
}

function dependencyOffsets(text, key) {
  const offsets = [];
  for (const tag of scanGoSource(text).tags) {
    const dependency = tagAttributes(tag).get("inject");
    if (dependency?.value === key) {
      offsets.push(dependency.index);
    }
  }
  return offsets;
}

module.exports = {
  contractAt,
  contractMarkers,
  contractOffsets,
  contractsOfType,
  declarationOffsets,
  dependencyAt,
  dependencyOffsets,
  enclosingTypeAt,
  parseAnalyzerOutput,
  positionAt,
  providerKeyAt,
  providerOffsets,
  scanMetadata,
  typeDeclarationAt,
  typeDeclarations,
  typeUsageOffsets,
  validatePackagePatterns,
};
