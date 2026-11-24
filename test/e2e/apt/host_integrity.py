"""Selected package-state evidence, not whole-host integrity.

No package tools are invoked. Selected paths and their ancestors cannot be
symlinks. Links within info are recorded as link text, never dereferenced; their
targets are not attested. Two matching snapshots detect observed instability,
not changes after return or changes restored between observations.
"""
import hashlib
import json
import os
from pathlib import Path
import stat

# Constants are patchable only by offline fixture tests; no CLI/env selection.
REQUIRED_FILES = ("/var/lib/dpkg/status",)
OPTIONAL_FILES = ("/var/lib/dpkg/status-old", "/var/lib/dpkg/available",
                  "/var/lib/apt/extended_states")
INFO_ROOT = "/var/lib/dpkg/info"
EXCLUSIONS = ["/var/lib/dpkg/lock", "/var/lib/dpkg/lock-frontend",
              "/var/lib/apt/lists (including partial and lock)",
              "/var/cache/apt (including archives, partial and lock)",
              "/etc/apt (including credentials)",
              "all paths outside the selected files and info tree"]
LIMITATIONS = ["Selected state only; not entire host, filesystem or package-store integrity.",
               "Info symlinks attest link text only, not target content.",
               "Matching snapshots cannot detect changes restored between observations or after return.",
               "Absent optional files are recorded as absent, not hashed."]


class UnstableEvidence(ValueError):
    """A selected object changed during evidence capture."""


def _metadata(node):
    return {key: getattr(node, "st_" + key) for key in
            ("dev", "ino", "mode", "uid", "gid", "nlink", "size", "mtime_ns", "ctime_ns")}


def _same(before, after):
    if _metadata(before) != _metadata(after):
        raise UnstableEvidence("selected evidence changed during capture")


def _parent(path):
    """Open every ancestor with no-follow, returning an owned parent fd."""
    path = Path(path)
    if not path.is_absolute() or ".." in path.parts or path.name in ("", "/"):
        raise ValueError("selected path must be an absolute leaf path")
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    fd = os.open("/", flags)
    try:
        for part in path.parts[1:-1]:
            child = os.open(part, flags, dir_fd=fd)
            os.close(fd)
            fd = child
        return fd, path.name
    except BaseException:
        os.close(fd)
        raise


def _node(parent, name, allow_link=False, selected_node=None):
    before = os.stat(name, dir_fd=parent, follow_symlinks=False)
    if selected_node is not None:
        _same(selected_node, before)
    metadata = _metadata(before)
    if stat.S_ISLNK(before.st_mode):
        if not allow_link:
            raise ValueError("selected root/file is a symlink")
        target = os.readlink(name, dir_fd=parent)
        _same(before, os.stat(name, dir_fd=parent, follow_symlinks=False))
        return {"kind": "symlink", "metadata": metadata, "target": target}
    directory = stat.S_ISDIR(before.st_mode)
    if not directory and not stat.S_ISREG(before.st_mode):
        raise ValueError("nonregular selected evidence")
    flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
    if directory:
        flags |= os.O_DIRECTORY
    fd = os.open(name, flags, dir_fd=parent)
    try:
        _same(before, os.fstat(fd))
        if directory:
            # scandir errors propagate, including iteration failures.
            with os.scandir(fd) as entries:
                names = sorted(entry.name for entry in entries)
            children = {child: _node(fd, child, allow_link=True) for child in names}
            with os.scandir(fd) as entries:
                if names != sorted(entry.name for entry in entries):
                    raise UnstableEvidence("selected tree inventory changed during capture")
            result = {"kind": "directory", "metadata": metadata, "children": children}
        else:
            digest = hashlib.sha256()
            while chunk := os.read(fd, 1024 * 1024):
                digest.update(chunk)
            result = {"kind": "file", "metadata": metadata, "sha256": digest.hexdigest()}
        _same(before, os.fstat(fd))
        _same(before, os.stat(name, dir_fd=parent, follow_symlinks=False))
        return result
    finally:
        os.close(fd)


def _selected(path, optional=False, directory=False):
    parent, name = _parent(path)
    try:
        # Only absence of the optional leaf is permitted, not missing ancestors
        # or a missing child inside a present selected tree.
        try:
            node = os.stat(name, dir_fd=parent, follow_symlinks=False)
        except FileNotFoundError:
            if optional:
                return {"kind": "absent"}
            raise
        expected_type = stat.S_ISDIR if directory else stat.S_ISREG
        if not expected_type(node.st_mode):
            raise ValueError("selected evidence has wrong object type or is a symlink")
        return _node(parent, name, selected_node=node)
    finally:
        os.close(parent)


def _snapshot(optional_files=()):
    files = {path: _selected(path) for path in REQUIRED_FILES}
    files.update({path: _selected(path, optional=True) for path in optional_files})
    return {"files": files, "info": _selected(INFO_ROOT, directory=True)}


def host_hashes(*, optional_files=None):
    """Return selected evidence; optional_files=() excludes all optional reads.

    None preserves the historical optional scope. Explicit selections must be
    unique members of OPTIONAL_FILES; invalid scope fails before any read.
    Missing/unreadable required evidence and traversal/hash failures raise
    OSError; unsupported types and observed races raise ValueError subclasses.
    Stable metadata omits atime, which reads can themselves update.
    """
    selected = OPTIONAL_FILES if optional_files is None else optional_files
    if (not isinstance(selected, (tuple, list)) or
            any(not isinstance(p, str) or p not in OPTIONAL_FILES for p in selected) or
            len(set(selected)) != len(selected)):
        raise ValueError("optional integrity selection outside authorized known scope")
    first = _snapshot(selected)
    if first != _snapshot(selected):
        raise UnstableEvidence("selected evidence changed between snapshots")
    counts = {"files": 0, "directories": 0, "symlinks": 0, "absent_optional_files": 0}

    def count(node):
        kind = node["kind"]
        if kind == "absent":
            counts["absent_optional_files"] += 1
        else:
            counts[{"file": "files", "directory": "directories", "symlink": "symlinks"}[kind]] += 1
        for child in node.get("children", {}).values():
            count(child)

    for node in first["files"].values():
        count(node)
    count(first["info"])
    content = json.dumps(first, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()
    return {"schema": "host-selected-state-v1",
            "scope": {"required_files": list(REQUIRED_FILES),
                      "optional_files": list(selected), "complete_tree": INFO_ROOT},
            "exclusions": list(EXCLUSIONS), "limitations": list(LIMITATIONS),
            "counts": counts, "snapshot_sha256": hashlib.sha256(content).hexdigest(),
            "evidence": first}
