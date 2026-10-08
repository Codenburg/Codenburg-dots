import { constants } from "node:fs";
import { createHash, randomBytes } from "node:crypto";
import { lstat, open, realpath, rename, unlink } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const START_MARKER = "<!-- agents-md-manager:managed:start -->";
export const END_MARKER = "<!-- agents-md-manager:managed:end -->";
export const LIFECYCLE_STATES = Object.freeze({
  ABSENT: "ABSENT",
  MANAGED: "MANAGED",
  UNMANAGED: "UNMANAGED",
  MALFORMED: "MALFORMED",
});

const START_BYTES = Buffer.from(START_MARKER);
const END_BYTES = Buffer.from(END_MARKER);

export function getSafeOpenCapabilities(flagSource = constants) {
  const requiredFlags = ["O_NOFOLLOW", "O_NONBLOCK"];
  const missingFlags = requiredFlags.filter((name) => typeof flagSource[name] !== "number" || flagSource[name] === 0);
  if (missingFlags.length > 0) return Object.freeze({ available: false, missingFlags: Object.freeze(missingFlags) });
  const noMissingFlags = Object.freeze([]);
  return Object.freeze({
    available: true,
    missingFlags: noMissingFlags,
    readFlags: flagSource.O_RDONLY | flagSource.O_NOFOLLOW | flagSource.O_NONBLOCK,
    createFlags: flagSource.O_WRONLY | flagSource.O_CREAT | flagSource.O_EXCL | flagSource.O_NOFOLLOW | flagSource.O_NONBLOCK,
  });
}

export class AgentsMdRegionError extends Error {
  constructor(code, message, details = {}) {
    super(message);
    this.name = "AgentsMdRegionError";
    this.code = code;
    Object.assign(this, details);
  }
}

function fail(code, message, details = {}) {
  throw new AgentsMdRegionError(code, message, details);
}

function asBytes(value, label = "payload") {
  if (Buffer.isBuffer(value)) return Buffer.from(value);
  if (value instanceof Uint8Array) return Buffer.from(value);
  if (typeof value === "string") return Buffer.from(value);
  fail("INVALID_BYTES", `${label} must be a string, Buffer, or Uint8Array`);
}

function bytesEqual(left, right) {
  return Buffer.isBuffer(left) && Buffer.isBuffer(right) && left.equals(right);
}

function hidden(object, key, value) {
  Object.defineProperty(object, key, { value, enumerable: false, configurable: false });
  return object;
}

function scanMarkerLines(bytes) {
  const starts = [];
  const ends = [];
  const malformedLines = [];
  let lineStart = 0;

  for (let offset = 0; offset <= bytes.length; offset += 1) {
    if (offset !== bytes.length && bytes[offset] !== 0x0a) continue;

    const rawLineEnd = offset;
    const lineEndsWithLf = offset < bytes.length;
    const bodyEnd = lineEndsWithLf && rawLineEnd > lineStart && bytes[rawLineEnd - 1] === 0x0d ? rawLineEnd - 1 : rawLineEnd;
    const body = bytes.subarray(lineStart, bodyEnd);
    const isStart = body.equals(START_BYTES);
    const isEnd = body.equals(END_BYTES);

    if (isStart) starts.push({ start: lineStart, end: bodyEnd });
    if (isEnd) ends.push({ start: lineStart, end: bodyEnd });

    const hasStart = body.indexOf(START_BYTES) !== -1;
    const hasEnd = body.indexOf(END_BYTES) !== -1;
    if ((hasStart && !isStart) || (hasEnd && !isEnd) || (isStart && isEnd)) {
      malformedLines.push({ start: lineStart, end: bodyEnd, reason: "marker-line-not-exact" });
    }

    lineStart = offset + 1;
  }

  return { starts, ends, malformedLines };
}

export function classifyBytes(input) {
  const bytes = asBytes(input, "file bytes");
  const { starts, ends, malformedLines } = scanMarkerLines(bytes);
  const reasons = [];

  if (starts.length !== 1) reasons.push(starts.length === 0 ? "missing-start" : "duplicate-start");
  if (ends.length !== 1) reasons.push(ends.length === 0 ? "missing-end" : "duplicate-end");
  if (starts.length === 1 && ends.length === 1 && starts[0].start > ends[0].start) reasons.push("reversed-markers");
  if (malformedLines.length > 0) reasons.push("non-exact-marker-line");

  let state;
  if (starts.length === 0 && ends.length === 0 && malformedLines.length === 0) {
    state = LIFECYCLE_STATES.UNMANAGED;
  } else if (reasons.length === 0) {
    state = LIFECYCLE_STATES.MANAGED;
  } else {
    state = LIFECYCLE_STATES.MALFORMED;
  }

  const markers = {
    startCount: starts.length,
    endCount: ends.length,
    order: starts.length === 1 && ends.length === 1 ? (starts[0].start < ends[0].start ? "ordered" : "reversed") : "invalid",
    malformedLineCount: malformedLines.length,
    reasons,
  };
  const result = { state, markers };
  if (state === LIFECYCLE_STATES.MANAGED) {
    hidden(result, "payload", Buffer.from(bytes.subarray(starts[0].end, ends[0].start)));
    hidden(result, "parts", { prefixEnd: starts[0].end, suffixStart: ends[0].start });
  }
  return result;
}

export function resolveRoot(root = process.cwd()) {
  if (typeof root !== "string" || root.length === 0) fail("INVALID_ROOT", "root must be a nonempty path string");
  const resolvedRoot = path.resolve(root);
  const target = path.resolve(resolvedRoot, "AGENTS.md");
  const relativeTarget = path.relative(resolvedRoot, target);
  if (path.basename(target) !== "AGENTS.md" || relativeTarget !== "AGENTS.md") {
    fail("INVALID_TARGET", "the target is fixed to <root>/AGENTS.md");
  }
  return { root: resolvedRoot, target };
}

function kindOf(stats) {
  if (stats.isSymbolicLink()) return "symlink";
  if (stats.isDirectory()) return "directory";
  if (stats.isFIFO()) return "fifo";
  if (stats.isSocket()) return "socket";
  if (stats.isCharacterDevice()) return "character-device";
  if (stats.isBlockDevice()) return "block-device";
  if (stats.isFile()) return "file";
  return "special";
}

function unsafeFinding(scope, target, code, kind, detail = undefined) {
  return { scope, target, code, kind, ...(detail ? { detail } : {}) };
}

function unsafeResult(info, finding) {
  return {
    kind: "unsafe-target",
    safety: "unsafe",
    root: info.root,
    target: info.target,
    finding,
  };
}

function rootPathComponents(root) {
  const parsed = path.parse(root);
  const relative = path.relative(parsed.root, root);
  return relative.length === 0 ? [] : relative.split(path.sep).filter(Boolean);
}

function nodeIdentity(stats) {
  return { dev: stats.dev, ino: stats.ino, kind: kindOf(stats), modeType: stats.mode & 0o170000 };
}

async function inspectPhysicalRoot(info) {
  let current = path.parse(info.root).root;
  for (const component of rootPathComponents(info.root)) {
    current = path.join(current, component);
    let stats;
    try {
      stats = await lstat(current);
    } catch (error) {
      const code = error?.code === "ENOENT" ? (current === info.root ? "ROOT_ABSENT" : "ROOT_COMPONENT_UNAVAILABLE") : "ROOT_COMPONENT_UNAVAILABLE";
      return unsafeResult(info, unsafeFinding("root", current, code, "unavailable", error?.code));
    }
    const kind = kindOf(stats);
    if (kind === "symlink") return unsafeResult(info, unsafeFinding("root", current, "ROOT_SYMLINK_COMPONENT", kind));
    if (kind !== "directory") return unsafeResult(info, unsafeFinding("root", current, "ROOT_COMPONENT_NOT_DIRECTORY", kind));
  }

  let physicalRoot;
  try {
    physicalRoot = await realpath(info.root);
  } catch (error) {
    return unsafeResult(info, unsafeFinding("root", info.root, "ROOT_UNAVAILABLE", "unavailable", error?.code));
  }
  if (physicalRoot !== info.root) return unsafeResult(info, unsafeFinding("root", info.root, "ROOT_PATH_NOT_CANONICAL", "symlink"));

  const target = path.join(physicalRoot, "AGENTS.md");
  if (path.dirname(target) !== physicalRoot || path.basename(target) !== "AGENTS.md") {
    return unsafeResult(info, unsafeFinding("root", target, "INVALID_TARGET", "path"));
  }

  let rootStats;
  try {
    rootStats = await lstat(physicalRoot);
  } catch (error) {
    return unsafeResult(info, unsafeFinding("root", physicalRoot, "ROOT_UNAVAILABLE", "unavailable", error?.code));
  }
  if (kindOf(rootStats) !== "directory") return unsafeResult(info, unsafeFinding("root", physicalRoot, "ROOT_NOT_DIRECTORY", kindOf(rootStats)));
  return { info: { root: physicalRoot, target }, rootStats };
}

function identityEqual(left, right) {
  return left?.dev === right?.dev && left?.ino === right?.ino;
}

function snapshotOf(stats, bytes) {
  return {
    exists: true,
    dev: stats.dev,
    ino: stats.ino,
    mode: stats.mode,
    size: stats.size,
    bytes: Buffer.from(bytes),
  };
}

function absentSnapshot() {
  return { exists: false, bytes: Buffer.alloc(0) };
}

async function inspectTarget(info) {
  const capabilities = getSafeOpenCapabilities();
  if (!capabilities.available) {
    return unsafeResult(info, unsafeFinding("platform", info.target, "SAFE_OPEN_UNAVAILABLE", "capability", capabilities.missingFlags));
  }

  const physicalRoot = await inspectPhysicalRoot(info);
  if (physicalRoot.kind === "unsafe-target") return physicalRoot;
  info = physicalRoot.info;
  const rootStats = physicalRoot.rootStats;

  let targetStats;
  try {
    targetStats = await lstat(info.target);
  } catch (error) {
    if (error?.code === "ENOENT") {
      const result = {
        kind: "lifecycle",
        safety: "safe",
        root: info.root,
        target: info.target,
        state: LIFECYCLE_STATES.ABSENT,
        markers: { startCount: 0, endCount: 0, order: "none", malformedLineCount: 0, reasons: [] },
      };
      hidden(result, "bytes", Buffer.alloc(0));
      hidden(result, "snapshot", absentSnapshot());
      hidden(result, "rootSnapshot", nodeIdentity(rootStats));
      return result;
    }
    return unsafeResult(info, unsafeFinding("target", info.target, "TARGET_UNAVAILABLE", "unavailable", error?.code));
  }

  const targetKind = kindOf(targetStats);
  if (targetKind !== "file") {
    return unsafeResult(info, unsafeFinding("target", info.target, `TARGET_${targetKind.toUpperCase().replaceAll("-", "_")}`, targetKind));
  }

  let handle;
  try {
    handle = await open(info.target, capabilities.readFlags);
    const openedStats = await handle.stat();
    if (!openedStats.isFile() || !identityEqual(targetStats, openedStats)) {
      return unsafeResult(info, unsafeFinding("target", info.target, "TARGET_CHANGED_DURING_OPEN", kindOf(openedStats)));
    }
    const bytes = await handle.readFile();
    const finalStats = await handle.stat();
    if (finalStats.size !== bytes.length) {
      return unsafeResult(info, unsafeFinding("target", info.target, "TARGET_CHANGED_DURING_READ", "file"));
    }
    const parsed = classifyBytes(bytes);
    const result = { kind: "lifecycle", safety: "safe", root: info.root, target: info.target, state: parsed.state, markers: parsed.markers };
    hidden(result, "bytes", Buffer.from(bytes));
    hidden(result, "snapshot", snapshotOf(finalStats, bytes));
    hidden(result, "rootSnapshot", nodeIdentity(rootStats));
    if (parsed.state === LIFECYCLE_STATES.MANAGED) {
      hidden(result, "payload", parsed.payload);
      hidden(result, "parts", parsed.parts);
    }
    return result;
  } catch (error) {
    const code = error?.code === "ELOOP" ? "TARGET_SYMLINK_RACE" : "TARGET_READ_FAILED";
    return unsafeResult(info, unsafeFinding("target", info.target, code, "unavailable", error?.code));
  } finally {
    if (handle) await handle.close().catch(() => undefined);
  }
}

export async function classifyRoot(root = process.cwd()) {
  return inspectTarget(resolveRoot(root));
}

function requireSafeLifecycle(result) {
  if (result.kind !== "lifecycle" || result.safety !== "safe") {
    fail("UNSAFE_TARGET", "the fixed AGENTS.md target is unsafe", { finding: result.finding });
  }
}

function snapshotsEqual(expected, current) {
  if (!expected || !current || expected.exists !== current.exists) return false;
  if (!expected.exists) return true;
  return expected.dev === current.dev && expected.ino === current.ino && expected.mode === current.mode && bytesEqual(expected.bytes, current.bytes);
}

function identitiesEqual(left, right) {
  return left?.dev === right?.dev && left?.ino === right?.ino && left?.kind === right?.kind && left?.modeType === right?.modeType;
}

async function revalidateMutationBoundary(expected) {
  const current = await classifyRoot(expected.root);
  if (
    current.kind !== "lifecycle" ||
    current.root !== expected.root ||
    current.target !== expected.target ||
    current.state !== expected.state ||
    !identitiesEqual(current.rootSnapshot, expected.rootSnapshot) ||
    !snapshotsEqual(expected.snapshot, current.snapshot)
  ) {
    fail("RACE_DETECTED", "the root or fixed AGENTS.md target changed before the write", {
      expectedState: expected.state,
      currentState: current.state,
      currentFinding: current.finding,
    });
  }
  return current;
}

function managedBytes(payload) {
  const candidate = Buffer.concat([START_BYTES, asBytes(payload), END_BYTES]);
  const parsed = classifyBytes(candidate);
  if (parsed.state !== LIFECYCLE_STATES.MANAGED) {
    fail("INVALID_PAYLOAD", "payload must leave both exact markers on separate lines", { markers: parsed.markers });
  }
  return candidate;
}

function replacementBytes(initial, payload) {
  if (initial.state !== LIFECYCLE_STATES.MANAGED) fail("STATE_NOT_ALLOWED", "managed replacement requires MANAGED state", { state: initial.state });
  const candidatePayload = asBytes(payload);
  const candidate = Buffer.concat([
    initial.bytes.subarray(0, initial.parts.prefixEnd),
    candidatePayload,
    initial.bytes.subarray(initial.parts.suffixStart),
  ]);
  const parsed = classifyBytes(candidate);
  if (parsed.state !== LIFECYCLE_STATES.MANAGED) {
    fail("INVALID_PAYLOAD", "payload must leave both exact markers on separate lines", { markers: parsed.markers });
  }
  return { candidate, payload: candidatePayload };
}

function repairedDocumentBytes(document) {
  const candidate = asBytes(document, "document");
  const parsed = classifyBytes(candidate);
  if (parsed.state !== LIFECYCLE_STATES.MANAGED) {
    fail("INVALID_DOCUMENT", "repair document must contain exactly one ordered managed pair", { markers: parsed.markers });
  }
  return { candidate, payload: parsed.payload };
}

function separatorFor(original) {
  if (original.length === 0 || original[original.length - 1] === 0x0a) return Buffer.alloc(0);
  return Buffer.from("\n\n");
}

async function operationClassification(root) {
  const initial = await classifyRoot(root);
  requireSafeLifecycle(initial);
  return initial;
}

async function runBoundaryHook(options, boundary, classification) {
  if (options?.testBoundaryHook === undefined) return;
  if (typeof options.testBoundaryHook !== "function") fail("INVALID_BOUNDARY_HOOK", "testBoundaryHook must be a function");
  await options.testBoundaryHook(boundary, { root: classification.root, target: classification.target });
}

function errorInfo(error) {
  return { code: error?.code ?? "ERROR", message: error?.message ?? String(error) };
}

function surfaceCleanupFailure(operationError, tempTarget, cleanupError) {
  const cleanup = { status: "failed", path: tempTarget, ...errorInfo(cleanupError) };
  if (operationError && typeof operationError === "object") {
    operationError.cleanup = cleanup;
    return operationError;
  }
  return new AgentsMdRegionError("CLEANUP_FAILED", "operation failed and temporary-file cleanup failed", {
    operation: errorInfo(operationError),
    cleanup,
  });
}

async function atomicReplace(expected, bytes, options) {
  const capabilities = getSafeOpenCapabilities();
  if (!capabilities.available) fail("UNSAFE_TARGET", "safe-open capabilities are unavailable", { finding: { scope: "platform", code: "SAFE_OPEN_UNAVAILABLE", missingFlags: capabilities.missingFlags } });
  await runBoundaryHook(options, "before-temp-create", expected);
  await revalidateMutationBoundary(expected);

  let tempTarget;
  let handle;
  let renamed = false;
  let operationError;
  for (let attempt = 0; attempt < 5; attempt += 1) {
    tempTarget = path.join(expected.root, `.agents-md-manager-${randomBytes(12).toString("hex")}.tmp`);
    try {
      handle = await open(tempTarget, capabilities.createFlags, 0o600);
      break;
    } catch (error) {
      if (error?.code !== "EEXIST") throw error;
    }
  }
  if (!handle || !tempTarget) fail("TEMPORARY_FILE_FAILED", "could not create a same-directory temporary file");

  try {
    await handle.writeFile(bytes);
    await handle.chmod(expected.snapshot.mode & 0o7777);
    await handle.sync();
    await handle.close();
    handle = undefined;
    await runBoundaryHook(options, "before-final-rename", expected);
    await revalidateMutationBoundary(expected);
    await rename(tempTarget, expected.target);
    renamed = true;
    await runBoundaryHook(options, "after-final-rename", expected);
  } catch (error) {
    operationError = error;
    throw error;
  } finally {
    if (handle) await handle.close().catch(() => undefined);
    if (!renamed) {
      try {
        await unlink(tempTarget);
      } catch (cleanupError) {
        throw surfaceCleanupFailure(operationError, tempTarget, cleanupError);
      }
    }
  }
}

async function createAbsent(expected, bytes, options) {
  const capabilities = getSafeOpenCapabilities();
  if (!capabilities.available) fail("UNSAFE_TARGET", "safe-open capabilities are unavailable", { finding: { scope: "platform", code: "SAFE_OPEN_UNAVAILABLE", missingFlags: capabilities.missingFlags } });
  await runBoundaryHook(options, "before-final-create", expected);
  await revalidateMutationBoundary(expected);
  let handle;
  let createdStats;
  let failure;
  try {
    handle = await open(expected.target, capabilities.createFlags, 0o666);
    createdStats = await handle.stat();
    if (!createdStats.isFile()) fail("RACE_DETECTED", "the created target is not a regular file");
    await handle.writeFile(bytes);
    await handle.sync();
  } catch (error) {
    failure = error?.code === "EEXIST" ? new AgentsMdRegionError("RACE_DETECTED", "the fixed AGENTS.md target appeared before init") : error;
  } finally {
    if (handle) await handle.close().catch(() => undefined);
  }

  if (failure) {
    if (createdStats) {
      try {
        const currentStats = await lstat(expected.target);
        if (identityEqual(createdStats, currentStats)) await unlink(expected.target);
      } catch {
        // Cleanup is best effort; never remove a path that is no longer our inode.
      }
    }
    throw failure;
  }

  if (!createdStats) fail("WRITE_FAILED", "the absent target was not created");
}

function resultFor(operation, before, verification, extra = {}) {
  return {
    operation,
    stateBefore: before.state,
    stateAfter: verification.state,
    mutation: extra.mutation ?? "write",
    changed: extra.changed ?? true,
    filesChanged: extra.changed === false ? [] : ["AGENTS.md"],
    ...extra,
    verification,
  };
}

function assertVerified(verification) {
  if (!verification.ok) fail("VERIFY_FAILED", "post-operation verification failed", { verification });
}

function contentFingerprint(bytes) {
  const value = asBytes(bytes);
  return Object.freeze({ length: value.length, sha256: createHash("sha256").update(value).digest("hex") });
}

function fingerprintEqual(left, right) {
  return left?.length === right?.length && left?.sha256 === right?.sha256;
}

function identityFingerprint(identity) {
  return `${identity?.dev}:${identity?.ino}:${identity?.kind}:${identity?.modeType}`;
}

function makeVerificationToken(initial, candidate, payload, options = {}) {
  const token = {
    root: initial.root,
    target: initial.target,
    rootIdentity: identityFingerprint(initial.rootSnapshot),
    candidate: contentFingerprint(candidate),
    payload: contentFingerprint(payload),
  };
  if (options.preserveManaged) {
    token.prefix = contentFingerprint(initial.bytes.subarray(0, initial.parts.prefixEnd));
    token.suffix = contentFingerprint(initial.bytes.subarray(initial.parts.suffixStart));
  }
  if (options.preserveAdoptionPrefix) token.adoptionPrefix = contentFingerprint(initial.bytes);
  return Object.freeze(token);
}

function structuralVerification(actual) {
  const safeTarget = actual.kind === "lifecycle" && actual.safety === "safe";
  const markerCount = safeTarget ? { start: actual.markers.startCount, end: actual.markers.endCount } : { start: 0, end: 0 };
  const markerOrder = safeTarget && actual.state === LIFECYCLE_STATES.MANAGED && actual.markers.order === "ordered";
  const noSecondPair = markerCount.start === 1 && markerCount.end === 1 && markerOrder;
  return {
    ok: safeTarget && actual.state === LIFECYCLE_STATES.MANAGED && noSecondPair,
    safeTarget,
    state: safeTarget ? actual.state : undefined,
    rootIdentityPreserved: null,
    markerCount,
    markerOrder,
    noSecondPair,
    exactCandidate: null,
    exactPayload: null,
    prefixPreserved: null,
    suffixPreserved: null,
    adoptionPrefixPreserved: null,
  };
}

async function verifyWithToken(root, token) {
  const actual = await classifyRoot(root);
  const verification = structuralVerification(actual);
  if (!verification.safeTarget) return verification;

  verification.rootIdentityPreserved = actual.root === token.root && actual.target === token.target && identityFingerprint(actual.rootSnapshot) === token.rootIdentity;
  verification.exactCandidate = fingerprintEqual(contentFingerprint(actual.bytes), token.candidate);
  verification.exactPayload = actual.state === LIFECYCLE_STATES.MANAGED && fingerprintEqual(contentFingerprint(actual.payload), token.payload);
  if (token.prefix) {
    verification.prefixPreserved = actual.state === LIFECYCLE_STATES.MANAGED && fingerprintEqual(contentFingerprint(actual.bytes.subarray(0, actual.parts.prefixEnd)), token.prefix);
    verification.suffixPreserved = actual.state === LIFECYCLE_STATES.MANAGED && fingerprintEqual(contentFingerprint(actual.bytes.subarray(actual.parts.suffixStart)), token.suffix);
  }
  if (token.adoptionPrefix) {
    verification.adoptionPrefixPreserved = actual.bytes.length >= token.adoptionPrefix.length && fingerprintEqual(contentFingerprint(actual.bytes.subarray(0, token.adoptionPrefix.length)), token.adoptionPrefix);
  }
  verification.ok = verification.ok && verification.rootIdentityPreserved && verification.exactCandidate && verification.exactPayload && verification.prefixPreserved !== false && verification.suffixPreserved !== false && verification.adoptionPrefixPreserved !== false;
  return verification;
}

export async function verifyManagedRegion(root) {
  return structuralVerification(await classifyRoot(root));
}

export async function replaceManagedRegion(root, payload, options = {}) {
  const initial = await operationClassification(root);
  if (initial.state !== LIFECYCLE_STATES.MANAGED) fail("STATE_NOT_ALLOWED", "managed replacement requires MANAGED state", { state: initial.state });

  const replacement = replacementBytes(initial, payload);
  const token = makeVerificationToken(initial, replacement.candidate, replacement.payload, { preserveManaged: true });
  if (bytesEqual(replacement.payload, initial.payload)) {
    const verification = await verifyWithToken(initial.root, token);
    assertVerified(verification);
    return resultFor("update", initial, verification, { mutation: "none", changed: false, reason: "equivalent-payload" });
  }

  await atomicReplace(initial, replacement.candidate, options);
  const verification = await verifyWithToken(initial.root, token);
  assertVerified(verification);
  return resultFor("update", initial, verification, { mutation: "replace" });
}

export async function repairManagedDocument(root, document, options = {}) {
  const initial = await operationClassification(root);
  if (initial.state !== LIFECYCLE_STATES.MANAGED) fail("STATE_NOT_ALLOWED", "full-document repair requires MANAGED state", { state: initial.state });
  if (options.repairApproved !== true) fail("REPAIR_APPROVAL_REQUIRED", "full-document repair requires repairApproved: true for this operation");

  const repair = repairedDocumentBytes(document);
  const token = makeVerificationToken(initial, repair.candidate, repair.payload);
  if (bytesEqual(repair.candidate, initial.bytes)) {
    const verification = await verifyWithToken(initial.root, token);
    assertVerified(verification);
    return resultFor("repair", initial, verification, { mutation: "none", changed: false, reason: "equivalent-document" });
  }

  await atomicReplace(initial, repair.candidate, options);
  const verification = await verifyWithToken(initial.root, token);
  assertVerified(verification);
  return resultFor("repair", initial, verification, { mutation: "full-replace" });
}

export async function adoptUnmanaged(root, payload, options = {}) {
  const initial = await operationClassification(root);
  if (initial.state !== LIFECYCLE_STATES.UNMANAGED) fail("STATE_NOT_ALLOWED", "adoption requires UNMANAGED state", { state: initial.state });
  if (options.approved !== true) fail("ADOPTION_APPROVAL_REQUIRED", "adoption requires approved: true for this current workflow");

  const candidatePayload = asBytes(payload);
  const block = managedBytes(candidatePayload);
  const separator = separatorFor(initial.bytes);
  const candidate = Buffer.concat([initial.bytes, separator, block]);
  const parsed = classifyBytes(candidate);
  if (parsed.state !== LIFECYCLE_STATES.MANAGED) fail("INVALID_PAYLOAD", "adoption candidate did not produce one managed pair", { markers: parsed.markers });

  const token = makeVerificationToken(initial, candidate, candidatePayload, { preserveAdoptionPrefix: true });
  await atomicReplace(initial, candidate, options);
  const verification = await verifyWithToken(initial.root, token);
  assertVerified(verification);
  return resultFor("adoption", initial, verification, { mutation: "append", separator: separator.toString("utf8") });
}

export async function initAbsent(root, payload, options = {}) {
  const initial = await operationClassification(root);
  if (initial.state !== LIFECYCLE_STATES.ABSENT) fail("STATE_NOT_ALLOWED", "init requires ABSENT state", { state: initial.state });

  const candidatePayload = asBytes(payload);
  const candidate = managedBytes(candidatePayload);
  const token = makeVerificationToken(initial, candidate, candidatePayload);
  await createAbsent(initial, candidate, options);
  const verification = await verifyWithToken(initial.root, token);
  assertVerified(verification);
  return resultFor("init", initial, verification, { mutation: "create" });
}

function publicClassification(result) {
  return {
    kind: result.kind,
    safety: result.safety,
    root: result.root,
    target: result.target,
    ...(result.state ? { state: result.state, markers: result.markers } : { finding: result.finding }),
  };
}

async function main(argv) {
  const args = [...argv];
  const command = args.shift() ?? "classify";
  if (command === "--help" || command === "help") {
    process.stdout.write("Usage: agents-md-region.mjs classify [--root PATH]\n");
    return;
  }
  if (command !== "classify") fail("INVALID_COMMAND", "the mechanical CLI only supports classify");

  let root = process.cwd();
  while (args.length > 0) {
    const flag = args.shift();
    if (flag === "--root") {
      root = args.shift();
      if (!root) fail("INVALID_ROOT", "--root requires a path");
      continue;
    }
    fail("INVALID_ARGUMENT", `unsupported argument: ${flag}`);
  }
  process.stdout.write(`${JSON.stringify(publicClassification(await classifyRoot(root)), null, 2)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`${JSON.stringify({ error: { code: error.code ?? "ERROR", message: error.message } })}\n`);
    process.exitCode = 1;
  });
}
