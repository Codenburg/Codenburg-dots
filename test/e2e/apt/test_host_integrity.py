"""Strictly offline selected-state tests; fixture data stays in private evidence."""
import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import host_integrity as subject


ROOT = Path(__file__).resolve().parent / "evidence" / "host-integrity-unit"


class SelectedStateTests(unittest.TestCase):
    def setUp(self):
        self.spawn_guard = patch("subprocess.Popen", side_effect=AssertionError("external spawn forbidden"))
        self.spawn_guard.start()
        self.addCleanup(self.spawn_guard.stop)
        ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
        fixture = tempfile.TemporaryDirectory(prefix="fixture-", dir=ROOT)
        self.addCleanup(fixture.cleanup)
        self.base = Path(fixture.name)
        self.dpkg = self.base / "dpkg"
        self.dpkg.mkdir()
        self.info = self.dpkg / "info"
        self.info.mkdir()
        self.status = self.dpkg / "status"
        self.status.write_bytes(b"sanitized status\n")
        self.optional = (self.dpkg / "status-old", self.dpkg / "available",
                         self.base / "extended_states")
        (self.info / "example.list").write_bytes(b"/sanitized/path\n")
        for name, value in (("REQUIRED_FILES", (str(self.status),)),
                            ("OPTIONAL_FILES", tuple(map(str, self.optional))),
                            ("INFO_ROOT", str(self.info))):
            patcher = patch.object(subject, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)

    def capture(self):
        result = subject.host_hashes()
        self.assertEqual(json.loads(json.dumps(result)), result)
        return result

    def test_explicit_empty_optional_scope_never_reads_optional_paths(self):
        original = subject._selected
        touched = []
        def selected(path, **kwargs):
            touched.append(path)
            if path in tuple(map(str, self.optional)):
                raise AssertionError("optional read outside grant")
            return original(path, **kwargs)
        with patch.object(subject, "_selected", side_effect=selected):
            result = subject.host_hashes(optional_files=())
        self.assertEqual(result["scope"]["optional_files"], [])
        self.assertEqual(set(result["evidence"]["files"]), {str(self.status)})
        self.assertEqual(set(touched), {str(self.status), str(self.info)})
        self.assertEqual(result["counts"]["absent_optional_files"], 0)
        with patch.object(subject, "_selected", side_effect=AssertionError("unexpected read")):
            with self.assertRaises(ValueError):
                subject.host_hashes(optional_files=("/outside/grant",))

    def test_lock_exclusion_and_complete_selected_inventory(self):
        lock = self.dpkg / "lock"
        lock.write_bytes(b"never read this excluded lock")
        original = os.open
        def guarded_open(path, *args, **kwargs):
            if str(path) in (str(lock), "lock", "lock-frontend"):
                raise PermissionError("sanitized unreadable dpkg lock")
            return original(path, *args, **kwargs)
        with patch.object(subject.os, "open", side_effect=guarded_open):
            first = self.capture()
            self.assertEqual(first, self.capture())
        self.assertEqual(first["counts"], {"files": 2, "directories": 1,
                                          "symlinks": 0, "absent_optional_files": 3})
        self.assertEqual(first["scope"]["complete_tree"], str(self.info))
        self.assertIn("/var/lib/dpkg/lock", first["exclusions"])
        self.assertEqual(first["evidence"]["files"][str(self.status)]["sha256"],
                         hashlib.sha256(b"sanitized status\n").hexdigest())
        self.assertEqual(first["evidence"]["files"][str(self.optional[0])], {"kind": "absent"})
        self.assertNotIn("atime_ns", first["evidence"]["info"]["metadata"])
        (self.info / "example.list").write_bytes(b"changed selected content")
        self.assertNotEqual(first, self.capture())

    def test_present_optional_files_and_nested_empty_directories(self):
        for path in self.optional:
            path.write_bytes(b"optional content")
        (self.info / "nested").mkdir()
        (self.info / "nested" / "empty").mkdir()
        result = self.capture()
        self.assertEqual(result["counts"], {"files": 5, "directories": 3,
                                           "symlinks": 0, "absent_optional_files": 0})
        for path in self.optional:
            self.assertEqual(result["evidence"]["files"][str(path)]["kind"], "file")

    def test_missing_required_file_and_tree_fail_closed(self):
        self.status.rename(self.dpkg / "saved-status")
        with self.assertRaises(FileNotFoundError):
            self.capture()
        (self.dpkg / "saved-status").rename(self.status)
        self.info.rename(self.dpkg / "saved-info")
        with self.assertRaises(FileNotFoundError):
            self.capture()

    def test_unreadable_selected_file_propagates(self):
        original = os.open
        def unreadable(path, *args, **kwargs):
            if path == "status":
                raise PermissionError("controlled selected read denial")
            return original(path, *args, **kwargs)
        with patch.object(subject.os, "open", side_effect=unreadable):
            with self.assertRaisesRegex(PermissionError, "controlled selected read denial"):
                self.capture()

    def test_hash_read_error_propagates(self):
        with patch.object(subject.os, "read", side_effect=OSError("controlled hash read failure")):
            with self.assertRaisesRegex(OSError, "controlled hash read failure"):
                self.capture()

    def test_tree_traversal_error_propagates(self):
        with patch.object(subject.os, "scandir", side_effect=PermissionError("controlled traversal denial")):
            with self.assertRaisesRegex(PermissionError, "controlled traversal denial"):
                self.capture()

    def test_tree_iteration_error_propagates(self):
        class BrokenIterator:
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def __iter__(self):
                raise OSError("controlled iteration failure")
        with patch.object(subject.os, "scandir", return_value=BrokenIterator()):
            with self.assertRaisesRegex(OSError, "controlled iteration failure"):
                self.capture()

    def test_file_change_during_read_fails_closed(self):
        original = os.read
        changed = False
        def racing_read(fd, size):
            nonlocal changed
            chunk = original(fd, size)
            if not changed:
                changed = True
                self.status.write_bytes(b"changed while reading")
            return chunk
        with patch.object(subject.os, "read", side_effect=racing_read):
            with self.assertRaises(subject.UnstableEvidence):
                self.capture()

    def test_directory_inventory_race_fails_closed(self):
        original = os.scandir
        calls = 0
        def racing_scan(fd):
            nonlocal calls
            calls += 1
            if calls == 2:
                (self.info / "new-entry").write_bytes(b"race")
            return original(fd)
        with patch.object(subject.os, "scandir", side_effect=racing_scan):
            with self.assertRaises(subject.UnstableEvidence):
                self.capture()

    def test_optional_absence_change_between_snapshots_fails_closed(self):
        original = os.stat
        calls = 0
        def racing_stat(path, *args, **kwargs):
            nonlocal calls
            if path == "status":
                calls += 1
                if calls == 4:
                    self.optional[0].write_bytes(b"appeared between snapshots")
            return original(path, *args, **kwargs)
        with patch.object(subject.os, "stat", side_effect=racing_stat):
            with self.assertRaises(subject.UnstableEvidence):
                self.capture()

    def test_tree_links_inventory_text_without_following_targets(self):
        (self.info / "dangling").symlink_to("missing-target")
        (self.info / "directory-link").symlink_to(".", target_is_directory=True)
        result = self.capture()
        self.assertEqual(result["counts"]["symlinks"], 2)
        children = result["evidence"]["info"]["children"]
        self.assertEqual(children["dangling"]["target"], "missing-target")
        self.assertEqual(children["directory-link"]["target"], ".")
        self.assertIn("Info symlinks attest link text only, not target content.", result["limitations"])

    def test_selected_files_root_and_ancestors_reject_symlinks(self):
        self.status.rename(self.dpkg / "real-status")
        self.status.symlink_to("real-status")
        with self.assertRaises(ValueError):
            self.capture()
        self.status.unlink()
        (self.dpkg / "real-status").rename(self.status)
        self.info.rename(self.dpkg / "real-info")
        self.info.symlink_to("real-info", target_is_directory=True)
        with self.assertRaises(ValueError):
            self.capture()
        self.info.unlink()
        (self.dpkg / "real-info").rename(self.info)
        linked = self.base / "linked-dpkg"
        linked.symlink_to("dpkg", target_is_directory=True)
        with patch.object(subject, "REQUIRED_FILES", (str(linked / "status"),)):
            with self.assertRaises(OSError):
                self.capture()

    def test_optional_symlink_and_wrong_type_are_not_absence(self):
        self.optional[0].symlink_to("missing-target")
        with self.assertRaises(ValueError):
            self.capture()
        self.optional[0].unlink()
        self.optional[0].mkdir()
        with self.assertRaises(ValueError):
            self.capture()

    def test_missing_optional_parent_is_not_optional_absence(self):
        missing = self.base / "missing-parent" / "extended_states"
        with patch.object(subject, "OPTIONAL_FILES", (str(missing),)):
            with self.assertRaises(FileNotFoundError):
                self.capture()

    def test_wrong_selected_type_rejected_before_traversal(self):
        self.status.rename(self.dpkg / "saved-status")
        self.status.mkdir()
        with patch.object(subject.os, "scandir", side_effect=AssertionError("must not traverse wrong type")):
            with self.assertRaises(ValueError):
                self.capture()

    def test_selected_type_race_rejected_before_traversal(self):
        original = os.stat
        calls = 0
        def racing_stat(path, *args, **kwargs):
            nonlocal calls
            if path == "status":
                calls += 1
                if calls == 2:
                    self.status.rename(self.dpkg / "saved-status")
                    self.status.mkdir()
            return original(path, *args, **kwargs)
        with patch.object(subject.os, "stat", side_effect=racing_stat), \
             patch.object(subject.os, "scandir", side_effect=AssertionError("must not traverse raced type")):
            with self.assertRaises(subject.UnstableEvidence):
                self.capture()

    def test_disappearing_tree_child_is_not_optional_absence(self):
        original = os.stat
        def racing_stat(path, *args, **kwargs):
            if path == "example.list":
                raise FileNotFoundError("controlled disappeared child")
            return original(path, *args, **kwargs)
        with patch.object(subject.os, "stat", side_effect=racing_stat):
            with self.assertRaisesRegex(FileNotFoundError, "controlled disappeared child"):
                self.capture()

    def test_tree_nonregular_file_fails_closed_without_blocking(self):
        os.mkfifo(self.info / "fifo")
        with self.assertRaisesRegex(ValueError, "nonregular selected evidence"):
            self.capture()

    def test_selected_file_replaced_before_open_fails_closed(self):
        original = os.open
        def racing_open(path, *args, **kwargs):
            if path == "status":
                self.status.rename(self.dpkg / "old-status")
                self.status.write_bytes(b"replacement")
            return original(path, *args, **kwargs)
        with patch.object(subject.os, "open", side_effect=racing_open):
            with self.assertRaises(subject.UnstableEvidence):
                self.capture()

    def test_symlink_change_during_readlink_fails_closed(self):
        link = self.info / "link"
        link.symlink_to("first")
        original = os.readlink
        def racing_readlink(path, *args, **kwargs):
            text = original(path, *args, **kwargs)
            link.unlink()
            link.symlink_to("second")
            return text
        with patch.object(subject.os, "readlink", side_effect=racing_readlink):
            with self.assertRaises(subject.UnstableEvidence):
                self.capture()


if __name__ == "__main__":
    unittest.main()
