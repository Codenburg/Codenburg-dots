import assert from "node:assert/strict";
import { constants } from "node:fs";
import { chmod, lstat, mkdir, mkdtemp, readFile, readdir, rename, rm, symlink, writeFile } from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  END_MARKER,
  LIFECYCLE_STATES,
  START_MARKER,
  adoptUnmanaged,
  classifyRoot,
  getSafeOpenCapabilities,
  initAbsent,
  repairManagedDocument,
  replaceManagedRegion,
  resolveRoot,
} from "./agents-md-region.mjs";

const start = Buffer.from(START_MARKER);
const end = Buffer.from(END_MARKER);

async function withRoot(callback) {
  const root = await mkdtemp(path.join(os.tmpdir(), "agents-md-region-"));
  try {
    return await callback(root);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

function managed(payload = Buffer.from("\nCommands\n")) {
  return Buffer.concat([start, payload, end]);
}

async function targetBytes(root) {
  return readFile(resolveRoot(root).target);
}

async function writeTarget(root, bytes, mode = undefined) {
  const target = resolveRoot(root).target;
  await writeFile(target, bytes);
  if (mode !== undefined) await chmod(target, mode);
  return target;
}

async function swapRootDirectory(root) {
  const moved = `${root}-moved`;
  await rename(root, moved);
  await mkdir(root);
  return moved;
}

async function statIdentity(target) {
  const stats = await lstat(target);
  return { dev: stats.dev, ino: stats.ino, mode: stats.mode, size: stats.size, mtimeNs: stats.mtimeNs };
}

test("classifies an absent fixed root target", async () => {
  await withRoot(async (root) => {
    const result = await classifyRoot(root);
    assert.equal(result.kind, "lifecycle");
    assert.equal(result.state, LIFECYCLE_STATES.ABSENT);
    assert.equal(result.safety, "safe");
  });
});

test("classifies one exact ordered managed pair", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, managed());
    const result = await classifyRoot(root);
    assert.equal(result.state, LIFECYCLE_STATES.MANAGED);
    assert.deepEqual(result.markers, { startCount: 1, endCount: 1, order: "ordered", malformedLineCount: 0, reasons: [] });
    assert.deepEqual(result.payload, Buffer.from("\nCommands\n"));
  });
});

test("classifies a regular file without markers as unmanaged", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from("manual instructions\n"));
    const result = await classifyRoot(root);
    assert.equal(result.state, LIFECYCLE_STATES.UNMANAGED);
  });
});

test("classifies a missing start marker as malformed", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from(`${END_MARKER}\n`));
    assert.equal((await classifyRoot(root)).state, LIFECYCLE_STATES.MALFORMED);
  });
});

test("classifies a missing end marker as malformed", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from(`${START_MARKER}\nbody\n`));
    assert.equal((await classifyRoot(root)).state, LIFECYCLE_STATES.MALFORMED);
  });
});

test("classifies duplicate start markers as malformed", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from(`${START_MARKER}\n${START_MARKER}\nbody\n${END_MARKER}`));
    const result = await classifyRoot(root);
    assert.equal(result.state, LIFECYCLE_STATES.MALFORMED);
    assert.ok(result.markers.reasons.includes("duplicate-start"));
  });
});

test("classifies duplicate end markers as malformed", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from(`${START_MARKER}\nbody\n${END_MARKER}\n${END_MARKER}`));
    const result = await classifyRoot(root);
    assert.equal(result.state, LIFECYCLE_STATES.MALFORMED);
    assert.ok(result.markers.reasons.includes("duplicate-end"));
  });
});

test("classifies reversed markers as malformed", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from(`${END_MARKER}\nbody\n${START_MARKER}`));
    const result = await classifyRoot(root);
    assert.equal(result.state, LIFECYCLE_STATES.MALFORMED);
    assert.ok(result.markers.reasons.includes("reversed-markers"));
  });
});

test("classifies nested marker pairs as malformed", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from(`${START_MARKER}\nouter\n${START_MARKER}\ninner\n${END_MARKER}\nouter\n${END_MARKER}`));
    const result = await classifyRoot(root);
    assert.equal(result.state, LIFECYCLE_STATES.MALFORMED);
    assert.ok(result.markers.reasons.includes("duplicate-start"));
    assert.ok(result.markers.reasons.includes("duplicate-end"));
  });
});

test("rejects marker text that is not an exact marker line", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from(`prefix ${START_MARKER}\nbody\n`));
    const result = await classifyRoot(root);
    assert.equal(result.state, LIFECYCLE_STATES.MALFORMED);
    assert.ok(result.markers.reasons.includes("non-exact-marker-line"));
  });
});

test("rejects a symlink component anywhere in the supplied root path", async () => {
  await withRoot(async (root) => {
    const external = path.join(root, "external");
    const linked = path.join(root, "link");
    const externalSub = path.join(external, "sub");
    await mkdir(externalSub, { recursive: true });
    await symlink(external, linked);
    await writeFile(path.join(externalSub, "AGENTS.md"), managed());
    const result = await classifyRoot(path.join(linked, "sub"));
    assert.equal(result.kind, "unsafe-target");
    assert.equal(result.finding.code, "ROOT_SYMLINK_COMPONENT");
  });
});

test("classifies an end marker followed by bare CR at EOF as malformed", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, Buffer.from(`${END_MARKER}\r`));
    const result = await classifyRoot(root);
    assert.equal(result.state, LIFECYCLE_STATES.MALFORMED);
    assert.ok(result.markers.reasons.includes("non-exact-marker-line"));
  });
});

test("replaces only the payload and preserves exact prefix and suffix", async () => {
  await withRoot(async (root) => {
    const prefix = Buffer.from("manual prefix  \t\n");
    const oldPayload = Buffer.from("\nOld route  \t\n");
    const suffix = Buffer.from("\nmanual suffix  \t\r\n");
    await writeTarget(root, Buffer.concat([prefix, start, oldPayload, end, suffix]));
    const newPayload = Buffer.from("\nNew route\n");
    const result = await replaceManagedRegion(root, newPayload);
    assert.equal(result.changed, true);
    assert.equal(result.mutation, "replace");
    assert.deepEqual(await targetBytes(root), Buffer.concat([prefix, start, newPayload, end, suffix]));
    assert.equal(result.verification.prefixPreserved, true);
    assert.equal(result.verification.suffixPreserved, true);
    assert.equal(result.verification.noSecondPair, true);
  });
});

test("requires explicit opt-in before full-document repair and keeps default exterior preservation", async () => {
  await withRoot(async (root) => {
    const original = Buffer.concat([Buffer.from("obsolete prefix\n"), managed(Buffer.from("\nOld\n")), Buffer.from("\nobsolete suffix\n")]);
    const repaired = Buffer.concat([Buffer.from("repaired prefix\n"), managed(Buffer.from("\nNew\n")), Buffer.from("\nrepaired suffix\n")]);
    await writeTarget(root, original);

    await assert.rejects(() => repairManagedDocument(root, repaired), (error) => error.code === "REPAIR_APPROVAL_REQUIRED");
    assert.deepEqual(await targetBytes(root), original);

    await replaceManagedRegion(root, Buffer.from("\nDefault update\n"));
    assert.deepEqual(await targetBytes(root), Buffer.concat([Buffer.from("obsolete prefix\n"), managed(Buffer.from("\nDefault update\n")), Buffer.from("\nobsolete suffix\n")]));
  });
});

test("repairs a managed document only with explicit approval and is idempotent", async () => {
  await withRoot(async (root) => {
    const original = Buffer.concat([Buffer.from("obsolete prefix\n"), managed(Buffer.from("\nOld\n")), Buffer.from("\nobsolete suffix\n")]);
    const repaired = Buffer.concat([Buffer.from("repaired prefix\n"), managed(Buffer.from("\nNew\n")), Buffer.from("\nrepaired suffix\n")]);
    await writeTarget(root, original);

    const first = await repairManagedDocument(root, repaired, { repairApproved: true });
    assert.equal(first.operation, "repair");
    assert.equal(first.mutation, "full-replace");
    assert.equal(first.verification.prefixPreserved, null);
    assert.equal(first.verification.suffixPreserved, null);
    assert.equal(first.verification.noSecondPair, true);
    assert.deepEqual(await targetBytes(root), repaired);

    const beforeStat = await statIdentity(resolveRoot(root).target);
    const second = await repairManagedDocument(root, repaired, { repairApproved: true });
    assert.equal(second.mutation, "none");
    assert.equal(second.changed, false);
    assert.deepEqual(await statIdentity(resolveRoot(root).target), beforeStat);
  });
});

test("rejects malformed and unsafe targets for full-document repair", async () => {
  await withRoot(async (root) => {
    const document = managed(Buffer.from("\nNew\n"));
    const invalidDocument = Buffer.from(`${START_MARKER}\n${START_MARKER}\nbody\n${END_MARKER}`);
    const managedOriginal = managed(Buffer.from("\nOld\n"));
    await writeTarget(root, managedOriginal);
    await assert.rejects(() => repairManagedDocument(root, invalidDocument, { repairApproved: true }), (error) => error.code === "INVALID_DOCUMENT");
    assert.deepEqual(await targetBytes(root), managedOriginal);

    const malformed = Buffer.from(`${START_MARKER}\n${START_MARKER}\nbody\n${END_MARKER}`);
    await writeTarget(root, malformed);
    await assert.rejects(() => repairManagedDocument(root, document, { repairApproved: true }), (error) => error.code === "STATE_NOT_ALLOWED");
    assert.deepEqual(await targetBytes(root), malformed);
  });

  await withRoot(async (root) => {
    const outside = path.join(root, "outside.txt");
    await writeFile(outside, Buffer.from("outside"));
    await symlink(outside, resolveRoot(root).target);
    await assert.rejects(() => repairManagedDocument(root, managed(Buffer.from("\nNew\n")), { repairApproved: true }), (error) => error.code === "UNSAFE_TARGET");
  });
});

test("derives preservation boundaries from fresh state instead of caller-mutated classification", async () => {
  await withRoot(async (root) => {
    const manualPrefix = Buffer.from("manual prefix\n");
    const manualSuffix = Buffer.from("\nmanual suffix");
    await writeTarget(root, Buffer.concat([manualPrefix, start, Buffer.from("\nOld\n"), end, manualSuffix]));
    const classification = await classifyRoot(root);
    classification.bytes.set(Buffer.from(START_MARKER), 0);
    classification.parts.prefixEnd = Buffer.from(START_MARKER).length;
    const result = await replaceManagedRegion(root, Buffer.from("\nNew\n"), { expected: classification });
    assert.equal(result.verification.prefixPreserved, true);
    assert.deepEqual(await targetBytes(root), Buffer.concat([manualPrefix, start, Buffer.from("\nNew\n"), end, manualSuffix]));
  });
});

test("does not write an equivalent payload and preserves stat identity and mtime", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, managed(Buffer.from("\nCurrent wording\n")));
    const target = resolveRoot(root).target;
    const beforeBytes = await targetBytes(root);
    const beforeStat = await statIdentity(target);
    const classification = await classifyRoot(root);
    const result = await replaceManagedRegion(root, classification.payload);
    const afterStat = await statIdentity(target);
    assert.equal(result.changed, false);
    assert.equal(result.mutation, "none");
    assert.deepEqual(result.filesChanged, []);
    assert.deepEqual(await targetBytes(root), beforeBytes);
    assert.deepEqual(afterStat, beforeStat);
  });
});

test("preserves CRLF, mixed bytes, and trailing whitespace outside the payload", async () => {
  await withRoot(async (root) => {
    const manualPrefix = Buffer.from([0x80, 0xff, 0x0d, 0x0a, 0x20, 0x09, 0x0d, 0x0a]);
    const oldPayload = Buffer.from("\r\nCommands  \r\n");
    const manualSuffix = Buffer.from("\r\nmanual suffix  \t\r\ntrailing  \t");
    const original = Buffer.concat([manualPrefix, start, oldPayload, end, manualSuffix]);
    await writeTarget(root, original);
    const newPayload = Buffer.from("\r\nProject essentials  \r\nCommands  \r\n");
    const result = await replaceManagedRegion(root, newPayload);
    const expectedPrefix = Buffer.concat([manualPrefix, start]);
    const expectedSuffix = Buffer.concat([end, manualSuffix]);
    const actual = await targetBytes(root);
    assert.equal(result.verification.prefixPreserved, true);
    assert.equal(result.verification.suffixPreserved, true);
    assert.deepEqual(actual, Buffer.concat([expectedPrefix, newPayload, expectedSuffix]));
    assert.deepEqual(actual.subarray(0, expectedPrefix.length), expectedPrefix);
    assert.deepEqual(actual.subarray(actual.length - expectedSuffix.length), expectedSuffix);
  });
});

test("requires explicit approval before adopting unmanaged content", async () => {
  await withRoot(async (root) => {
    const original = Buffer.from("manual content\n");
    await writeTarget(root, original);
    await assert.rejects(() => adoptUnmanaged(root, Buffer.from("\nCommands\n")), (error) => error.code === "ADOPTION_APPROVAL_REQUIRED");
    assert.deepEqual(await targetBytes(root), original);
    assert.equal((await classifyRoot(root)).state, LIFECYCLE_STATES.UNMANAGED);
  });
});

test("adopts an empty unmanaged file without changing its prior bytes", async () => {
  await withRoot(async (root) => {
    const original = Buffer.alloc(0);
    await writeTarget(root, original);
    const result = await adoptUnmanaged(root, Buffer.from("\nCommands\n"), { approved: true });
    assert.equal(result.separator, "");
    const actual = await targetBytes(root);
    assert.deepEqual(actual.subarray(0, original.length), original);
    const classification = await classifyRoot(root);
    assert.equal(classification.markers.startCount, 1);
    assert.equal(classification.markers.endCount, 1);
  });
});

test("uses no separator when unmanaged content already ends in LF", async () => {
  await withRoot(async (root) => {
    const original = Buffer.from("manual\n");
    await writeTarget(root, original);
    const result = await adoptUnmanaged(root, Buffer.from("\nCommands\n"), { approved: true });
    assert.equal(result.separator, "");
    assert.deepEqual((await targetBytes(root)).subarray(0, original.length), original);
  });
});

test("uses exactly two LF bytes when unmanaged content lacks a trailing LF", async () => {
  await withRoot(async (root) => {
    const original = Buffer.from("manual");
    await writeTarget(root, original);
    const result = await adoptUnmanaged(root, Buffer.from("\nCommands\n"), { approved: true });
    assert.equal(result.separator, "\n\n");
    assert.equal(result.verification.adoptionPrefixPreserved, true);
    assert.equal(result.verification.noSecondPair, true);
    const actual = await targetBytes(root);
    assert.deepEqual(actual.subarray(0, original.length), original);
    assert.deepEqual(actual.subarray(original.length, original.length + 2), Buffer.from("\n\n"));
  });
});

test("fails verification when adoption separator changes after final rename", async () => {
  await withRoot(async (root) => {
    const original = Buffer.from("manual");
    const payload = Buffer.from("\nCommands\n");
    await writeTarget(root, original);
    await assert.rejects(
      () => adoptUnmanaged(root, payload, {
        approved: true,
        testBoundaryHook: async (boundary, { target }) => {
          if (boundary !== "after-final-rename") return;
          const actual = await readFile(target);
          const separator = Buffer.from("\n\n");
          const separatorStart = actual.indexOf(separator);
          assert.ok(separatorStart >= original.length);
          await writeFile(target, Buffer.concat([actual.subarray(0, separatorStart), Buffer.from("\n"), actual.subarray(separatorStart + separator.length)]));
        },
      }),
      (error) => {
        assert.equal(error.code, "VERIFY_FAILED");
        assert.equal(error.verification.exactCandidate, false);
        assert.equal(error.verification.adoptionPrefixPreserved, true);
        return true;
      },
    );
    assert.deepEqual(await targetBytes(root), Buffer.concat([original, Buffer.from("\n"), managed(payload)]));
  });
});

test("rejects a symlink target without following or reading it", async () => {
  await withRoot(async (root) => {
    const outside = path.join(root, "outside.txt");
    await writeFile(outside, Buffer.from("outside target bytes"));
    await symlink(outside, resolveRoot(root).target);
    const result = await classifyRoot(root);
    assert.equal(result.kind, "unsafe-target");
    assert.equal(result.finding.code, "TARGET_SYMLINK");
    assert.equal("state" in result, false);
  });
});

test("reports safe-open capability truthfully and fails closed when unavailable", async () => {
  const capability = getSafeOpenCapabilities();
  assert.equal(capability.available, true);
  assert.deepEqual(capability.missingFlags, []);
  assert.equal(typeof capability.readFlags, "number");
  assert.equal(typeof capability.createFlags, "number");
  assert.equal(capability.readFlags & constants.O_NOFOLLOW, constants.O_NOFOLLOW);
  assert.equal(capability.readFlags & constants.O_NONBLOCK, constants.O_NONBLOCK);
  assert.equal(capability.createFlags & constants.O_NOFOLLOW, constants.O_NOFOLLOW);
  assert.equal(capability.createFlags & constants.O_NONBLOCK, constants.O_NONBLOCK);

  const unavailable = getSafeOpenCapabilities({ O_RDONLY: constants.O_RDONLY, O_WRONLY: constants.O_WRONLY, O_CREAT: constants.O_CREAT, O_EXCL: constants.O_EXCL, O_NOFOLLOW: 0, O_NONBLOCK: constants.O_NONBLOCK });
  assert.deepEqual(unavailable, { available: false, missingFlags: ["O_NOFOLLOW"] });
});

test("rejects a directory target as an unsafe nonregular target", async () => {
  await withRoot(async (root) => {
    await mkdir(resolveRoot(root).target);
    const result = await classifyRoot(root);
    assert.equal(result.kind, "unsafe-target");
    assert.equal(result.finding.code, "TARGET_DIRECTORY");
  });
});

test("rejects a symlink root without following it", async () => {
  await withRoot(async (root) => {
    const linkedRoot = path.join(root, "linked-root");
    await symlink(root, linkedRoot);
    const result = await classifyRoot(linkedRoot);
    assert.equal(result.kind, "unsafe-target");
    assert.equal(result.finding.scope, "root");
    assert.equal(result.finding.code, "ROOT_SYMLINK_COMPONENT");
  });
});

test("rejects a Unix socket target when the platform provides Unix sockets", { skip: process.platform === "win32" }, async () => {
  await withRoot(async (root) => {
    const target = resolveRoot(root).target;
    const server = net.createServer();
    await new Promise((resolve, reject) => {
      server.once("error", reject);
      server.listen(target, resolve);
    });
    try {
      const result = await classifyRoot(root);
      assert.equal(result.kind, "unsafe-target");
      assert.equal(result.finding.code, "TARGET_SOCKET");
    } finally {
      await new Promise((resolve) => server.close(resolve));
    }
  });
});

test("derives only the fixed root AGENTS.md target", async () => {
  await withRoot(async (root) => {
    const nestedRoot = path.join(root, "nested");
    await mkdir(nestedRoot);
    const resolved = resolveRoot(nestedRoot);
    assert.equal(resolved.target, path.join(path.resolve(nestedRoot), "AGENTS.md"));
    assert.equal(path.dirname(resolved.target), resolved.root);
    assert.notEqual(resolved.target, path.join(root, "outside.md"));
  });
});

test("preserves the regular-file mode during replacement", async () => {
  await withRoot(async (root) => {
    const target = await writeTarget(root, managed(Buffer.from("\nOld\n")), 0o640);
    const beforeMode = (await lstat(target)).mode & 0o7777;
    await replaceManagedRegion(root, Buffer.from("\nNew\n"));
    const afterMode = (await lstat(target)).mode & 0o7777;
    assert.equal(afterMode, beforeMode);
  });
});

test("fails closed when the root identity swaps before temporary-file creation", async () => {
  await withRoot(async (root) => {
    const original = managed(Buffer.from("\nOld\n"));
    await writeTarget(root, original);
    let moved;
    try {
      await assert.rejects(
        () => replaceManagedRegion(root, Buffer.from("\nNew\n"), { testBoundaryHook: async (boundary) => { if (boundary === "before-temp-create") moved = await swapRootDirectory(root); } }),
        (error) => error.code === "RACE_DETECTED",
      );
      assert.ok(moved);
      assert.deepEqual(await readFile(path.join(moved, "AGENTS.md")), original);
      await assert.rejects(() => lstat(resolveRoot(root).target), (error) => error.code === "ENOENT");
    } finally {
      if (moved) await rm(moved, { recursive: true, force: true });
    }
  });
});

test("surfaces cleanup failure when the root identity swaps immediately before final rename", async () => {
  await withRoot(async (root) => {
    const original = managed(Buffer.from("\nOld\n"));
    await writeTarget(root, original);
    let moved;
    try {
      await assert.rejects(
        () => replaceManagedRegion(root, Buffer.from("\nNew\n"), { testBoundaryHook: async (boundary) => { if (boundary === "before-final-rename") moved = await swapRootDirectory(root); } }),
        (error) => {
          assert.equal(error.code, "RACE_DETECTED");
          assert.equal(error.cleanup.status, "failed");
          assert.equal(error.cleanup.code, "ENOENT");
          assert.equal(path.dirname(error.cleanup.path), root);
          return true;
        },
      );
      assert.ok(moved);
      assert.deepEqual(await readFile(path.join(moved, "AGENTS.md")), original);
      const strandedTemps = (await readdir(moved)).filter((entry) => entry.startsWith(".agents-md-manager-") && entry.endsWith(".tmp"));
      assert.equal(strandedTemps.length, 1);
      await assert.rejects(() => lstat(resolveRoot(root).target), (error) => error.code === "ENOENT");
    } finally {
      if (moved) await rm(moved, { recursive: true, force: true });
    }
  });
});

test("fails closed when the root identity swaps immediately before init create", async () => {
  await withRoot(async (root) => {
    let moved;
    try {
      await assert.rejects(
        () => initAbsent(root, Buffer.from("\nProject essentials\n"), { testBoundaryHook: async (boundary) => { if (boundary === "before-final-create") moved = await swapRootDirectory(root); } }),
        (error) => error.code === "RACE_DETECTED",
      );
      assert.ok(moved);
      await assert.rejects(() => lstat(path.join(moved, "AGENTS.md")), (error) => error.code === "ENOENT");
      await assert.rejects(() => lstat(resolveRoot(root).target), (error) => error.code === "ENOENT");
    } finally {
      if (moved) await rm(moved, { recursive: true, force: true });
    }
  });
});

test("creates an absent target safely with one managed pair", async () => {
  await withRoot(async (root) => {
    const result = await initAbsent(root, Buffer.from("\nProject essentials\n"));
    assert.equal(result.operation, "init");
    assert.equal(result.stateBefore, LIFECYCLE_STATES.ABSENT);
    assert.equal(result.stateAfter, LIFECYCLE_STATES.MANAGED);
    assert.equal(result.verification.noSecondPair, true);
    const classification = await classifyRoot(root);
    assert.equal(classification.markers.startCount, 1);
    assert.equal(classification.markers.endCount, 1);
  });
});

test("refuses init when a present target already exists", async () => {
  await withRoot(async (root) => {
    const original = Buffer.from("manual\n");
    await writeTarget(root, original);
    await assert.rejects(() => initAbsent(root, Buffer.from("\nCommands\n")), (error) => error.code === "STATE_NOT_ALLOWED");
    assert.deepEqual(await targetBytes(root), original);
  });
});

test("refuses replacement of malformed input without mutation", async () => {
  await withRoot(async (root) => {
    const original = Buffer.from(`${START_MARKER}\n${START_MARKER}\nbody\n${END_MARKER}`);
    await writeTarget(root, original);
    await assert.rejects(() => replaceManagedRegion(root, Buffer.from("\nNew\n")), (error) => error.code === "STATE_NOT_ALLOWED");
    assert.deepEqual(await targetBytes(root), original);
  });
});

test("requires payload boundary bytes and performs no partial normalization", async () => {
  await withRoot(async (root) => {
    const original = managed(Buffer.from("\nOld\n"));
    await writeTarget(root, original);
    await assert.rejects(() => replaceManagedRegion(root, Buffer.from("New")), (error) => error.code === "INVALID_PAYLOAD");
    assert.deepEqual(await targetBytes(root), original);
  });
});

test("returns a small deterministic verification result", async () => {
  await withRoot(async (root) => {
    await writeTarget(root, managed(Buffer.from("\nOld\n")));
    const result = await replaceManagedRegion(root, Buffer.from("\nNew\n"));
    assert.deepEqual(Object.keys(result.verification).sort(), [
      "adoptionPrefixPreserved",
      "exactCandidate",
      "exactPayload",
      "markerCount",
      "markerOrder",
      "noSecondPair",
      "ok",
      "prefixPreserved",
      "rootIdentityPreserved",
      "safeTarget",
      "state",
      "suffixPreserved",
    ].sort());
    assert.equal(result.verification.ok, true);
    assert.equal(result.verification.safeTarget, true);
    assert.equal(result.verification.exactCandidate, true);
    assert.equal(result.verification.exactPayload, true);
  });
});
