"""Offline proc-collector contract; fixtures are owned worktree files only."""
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import proc_evidence as subject


class ProcEvidenceTests(unittest.TestCase):
    def test_guest_frames_are_complete_and_unambiguous(self):
        raw = subject.GUEST_SCRIPT
        self.assertNotIn("apt", raw)
        self.assertNotIn("unshare", raw)
        chunks = []
        for frame in ("init", "collector"):
            for field in subject.FIELDS:
                chunks += [subject.marker(frame, field), field + "\n"]
            for name in subject.NAMESPACES:
                chunks += [subject.marker(frame, "ns-" + name), name + ":[42]\n"]
        text = "".join(chunks)
        bundle = subject.parse_guest(text)
        self.assertEqual(bundle["init"]["status"], "status\n")
        self.assertEqual(bundle["collector"]["namespaces"]["mnt"], "mnt:[42]")
        for broken in ("noise\n" + text, text + "noise\n", text.replace(chunks[0], "", 1),
                       text.replace(chunks[0], chunks[0] * 2, 1)):
            with self.subTest(broken=broken[:60]), self.assertRaises(ValueError):
                subject.parse_guest(broken)

    def test_invalid_pid_rejects_before_any_read(self):
        with patch.object(subject.os, "open", side_effect=AssertionError("unauthorized read")):
            for pid in (0, -1, True, "1", 1.5):
                with self.subTest(pid=pid), self.assertRaises(ValueError):
                    subject.collect_host(pid)

    def test_owned_process_frames_and_denied_objects(self):
        root = Path(__file__).resolve().parent / "evidence" / "parent-d1-offline"
        root.mkdir(mode=0o700, parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(dir=root) as directory:
            base = Path(directory)
            for pid in (111, 222):
                node = base / str(pid)
                (node / "ns").mkdir(parents=True)
                for field in subject.FIELDS:
                    (node / field).write_text(field + "\n")
                for name in subject.NAMESPACES:
                    (node / "ns" / name).symlink_to(name + ":[42]")
            with patch.object(subject, "PROC_ROOT", str(base)), patch.object(subject.os, "getpid", return_value=111), patch("subprocess.Popen", side_effect=AssertionError("spawn forbidden")):
                bundle = subject.collect_host(222)
                self.assertEqual(bundle["controller"]["uid_map"], "uid_map\n")
                self.assertEqual(bundle["init"]["namespaces"]["user"], "user:[42]")
                self.assertEqual(bundle["init_stat_after"], "stat\n")
                (base / "222" / "status").unlink()
                (base / "222" / "status").symlink_to(base / "111" / "status")
                with self.assertRaises(OSError):
                    subject.collect_host(222)
                (base / "222" / "status").unlink()
                (base / "222" / "status").write_bytes(b"x" * (subject.FIELD_LIMIT + 1))
                with self.assertRaises(ValueError):
                    subject.collect_host(222)


if __name__ == "__main__":
    unittest.main()
