"""Offline mount-validation regression tests; no runtime commands."""
import json
import re
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

from mount_checks import MountValidationError, validate_mountinfo

FIXTURES = Path(__file__).parent / "fixtures"
CID = "a" * 64


class MountChecksTests(unittest.TestCase):
    def setUp(self):
        self.spawn_guard = patch.object(
            subprocess, "Popen", side_effect=AssertionError("external process forbidden"))
        self.spawn_guard.start()
        self.addCleanup(self.spawn_guard.stop)
        self.text = (FIXTURES / "podman-4.9.3-mountinfo.txt").read_text()
        self.inspect = json.loads((FIXTURES / "podman-4.9.3-running.json").read_text())
        # Explicitly synthetic coordinates, not a captured controller snapshot.
        self.controller = "\n".join(line for line in (FIXTURES /
            "podman-4.9.3-controller-mountinfo-synthetic.txt").read_text().splitlines()
            if not line.startswith("#"))

    def full(self, text=None, **kwargs):
        values = dict(require_full=True, running_inspect=self.inspect,
                      graph_root="/synthetic-storage", run_root="/run/user/1000/containers", uid=1000,
                      controller_mountinfo=self.controller)
        values.update(kwargs)
        return validate_mountinfo(self.text if text is None else text, CID, **values)

    def canonical(self):
        # Controlled lexical evidence, not a claim about actual storage topology.
        lower = self.inspect["GraphDriver"]["Data"]["LowerDir"]
        return re.sub(r"lowerdir=[^,]+", "lowerdir=" + lower, self.text)

    def reject(self, text, message, **kwargs):
        before = json.dumps(self.inspect, sort_keys=True)
        with self.assertRaisesRegex(MountValidationError, message):
            self.full(text, **kwargs)
        self.assertEqual(json.dumps(self.inspect, sort_keys=True), before)

    def test_retained_standard_mounts_minimal(self):
        self.assertIsNone(validate_mountinfo(self.text, CID))

    def test_preserve_minimal_root_only_contract(self):
        self.assertIsNone(validate_mountinfo(
            "1 2 0:1 / / rw - overlay overlay rw", CID))

    def test_supported_aliases_are_opaque_diagnostics(self):
        self.assertIsNone(self.full())
        data = json.loads(json.dumps(self.inspect))
        data["GraphDriver"]["Data"]["LowerDir"] = "/synthetic-storage/overlay/l/LINK/../diff1"
        before = json.dumps(data, sort_keys=True)
        self.assertIsNone(self.full(running_inspect=data))
        self.assertEqual(json.dumps(data, sort_keys=True), before)
        # Plausible aliases never excuse a real boundary failure.
        self.reject(self.text.replace(" /sys ro,", " /sys rw,"), "missing effective mount protection")
        self.reject(self.text + "9999 789 0:99 / /var/lib/dpkg rw - tmpfs tmpfs rw\n",
                    "host share or unknown mount")

    def test_controller_evidence_required_only_in_full_mode(self):
        self.reject(self.canonical(), "missing or oversized controller mountinfo",
                    controller_mountinfo=None)
        self.assertIsNone(validate_mountinfo(self.text, CID, controller_mountinfo="invalid"))

    def test_exact_controller_coordinates(self):
        text = self.canonical()
        # A covering controller mount may itself have a non-root filesystem root.
        controller = self.controller.replace("0:59 / /run/user/1000", "0:59 /runtime /run/user/1000")
        translated = text.replace(" /containers/overlay-containers/", " /runtime/containers/overlay-containers/")
        self.assertIsNone(self.full(translated, controller_mountinfo=controller))
        self.reject(text, "unowned generated mount", controller_mountinfo=controller)
        for old, new in (("0:59", "0:60"), ("/userdata/hosts", "/userdata/xhosts"),
                         ("/userdata/hosts", "/userdata")):
            self.reject(text.replace(old, new, 1), "unowned generated mount")
        for controller in (self.controller + "\n" + self.controller.splitlines()[1],
                           self.controller.replace("2 1", "2 x"),
                           self.controller.replace(" / /run/user/1000", " /../runtime /run/user/1000"),
                           self.controller.replace("0:59", "broken"),
                           self.controller.replace(" - tmpfs", " shared:x - tmpfs")):
            with self.subTest(controller=controller):
                self.reject(text, "controller mountinfo", controller_mountinfo=controller)
        userdata = "/run/user/1000/containers/overlay-containers/" + CID + "/userdata/hosts"
        self.reject(text, "controller mountinfo runtime file is a mount directory",
                    controller_mountinfo=self.controller +
                    f"\n3 2 0:59 /containers {userdata} rw - tmpfs tmpfs rw")
        for root in ("/etc/hosts", "/arbitrary/hosts"):
            self.reject(text.replace("/containers/overlay-containers/" + CID +
                                    "/userdata/hosts", root), "unowned generated mount")
        # The root-only controller cannot corroborate a tmpfs file binding.
        self.reject(text, "unowned generated mount",
                    controller_mountinfo=self.controller.splitlines()[0])
        # A similarly prefixed mount must not cover the approved runtime path.
        self.reject(text, "unowned generated mount", controller_mountinfo=
                    self.controller.replace("/run/user/1000 ", "/run/user/1000-other "))

    def test_controlled_canonical_full_profile(self):
        self.assertIsNone(self.full(self.canonical()))

    def test_full_requires_every_boundary(self):
        lines = self.canonical().splitlines()
        for line in lines:
            target = line.split()[4]
            with self.subTest(target=target):
                text = "\n".join(other for other in lines if other != line)
                self.reject(text, "missing root mount" if target == "/" else
                            "missing required mount boundaries")
        self.reject("1 2 0:1 / / rw - overlay overlay rw", "missing required mount boundaries")

    def test_requires_fresh_runtime_inputs(self):
        text = self.canonical()
        for kwargs, message in (
            ({"uid": None}, "missing actual rootless UID"),
            ({"uid": True}, "missing actual rootless UID"),
            ({"uid": 0}, "missing actual rootless UID"),
            ({"graph_root": None}, "missing or broad graphRoot"),
            ({"graph_root": "/home"}, "missing or broad graphRoot"),
            ({"graph_root": "/home/me/../storage"}, "missing or broad graphRoot"),
            ({"run_root": None}, "unrecognized UID-bound runRoot"),
            ({"run_root": "/run/user/1001/containers"}, "unrecognized UID-bound runRoot"),
            ({"running_inspect": None}, "missing matching running inspect"),
        ):
            with self.subTest(kwargs=kwargs):
                self.reject(text, message, **kwargs)

    def test_inspect_paths_and_identity_are_bound(self):
        text = self.canonical()
        cases = (
            (("Id",), "b" * 64, "missing matching running inspect"),
            (("Driver",), "vfs", "missing matching running inspect"),
            (("State", "Running"), False, "missing matching running inspect"),
            (("Mounts",), [{"Source": "/home"}], "missing matching running inspect"),
            (("GraphDriver", "Name"), "vfs", "missing overlay inspect data"),
            (("GraphDriver", "Data", "UpperDir"), "/home/diff", "overlay upperdir"),
            (("GraphDriver", "Data", "WorkDir"), "/home/work", "overlay layer paths disagree"),
            (("GraphDriver", "Data", "MergedDir"), "/home/merged", "overlay layer paths disagree"),
            (("GraphDriver", "Data", "LowerDir"), "/var/lib/dpkg", "unrecognized inspected"),
            (("HostsPath",), "/home/hosts", "generated inspect path disagrees"),
            (("HostnamePath",), "/run/user/1001/containers/hostname", "generated inspect path disagrees"),
            (("ResolvConfPath",), "/tmp/resolv.conf", "generated inspect path disagrees"),
        )
        for keys, value, message in cases:
            data = json.loads(json.dumps(self.inspect))
            cursor = data
            for key in keys[:-1]:
                cursor = cursor[key]
            cursor[keys[-1]] = value
            with self.subTest(keys=keys):
                self.reject(text, message, running_inspect=data)

    def test_all_nonroot_tuples_and_effective_flags(self):
        lines = self.canonical().splitlines()
        for index, line in enumerate(lines):
            fields = line.split()
            target = fields[4]
            if target == "/":
                continue
            for column, value in ((3, "/home"), (7, "ext4"), (8, "/dev/sda1")):
                changed = fields.copy()
                changed[column] = value
                with self.subTest(target=target, column=column):
                    self.reject("\n".join(lines[:index] + [" ".join(changed)] + lines[index + 1:]),
                                "unowned generated mount|unrecognized mount tuple")
            for flag in fields[5].split(","):
                if flag == "relatime":
                    continue
                changed = fields.copy()
                flags = fields[5].split(",")
                flags.remove(flag)
                if flag in {"ro", "rw"}:
                    flags.append("rw" if flag == "ro" else "ro")
                changed[5] = ",".join(flags)
                with self.subTest(target=target, missing_flag=flag):
                    self.reject("\n".join(lines[:index] + [" ".join(changed)] + lines[index + 1:]),
                                "unsafe generated mount flags|missing effective mount protection")

    def test_generated_uid_and_target_binding(self):
        text = self.canonical()
        self.reject(text.replace("mode=700,uid=1000", "mode=700,uid=1001"),
                    "generated mount superblock is not UID-bound")
        self.reject(text.replace("/userdata/hosts", "/userdata/hostname", 1),
                    "unowned generated mount: /etc/hosts")
        self.reject(text.replace("/containers/overlay-containers/", "/home/overlay-containers/", 1),
                    "unowned generated mount")
        self.reject(text.replace(CID, "b" * 64, 1), "unowned generated mount")

    def test_overlay_substitutions_and_aliases(self):
        text = self.canonical()
        overlay = self.inspect["GraphDriver"]["Data"]
        for field, option in (("UpperDir", "upperdir"), ("WorkDir", "workdir")):
            with self.subTest(option=option):
                self.reject(text.replace(option + "=" + overlay[field], option + "=/home"),
                            "overlay upper/work disagree")
        for lower in ("/home", "/synthetic-storage/overlay/l/LINK/../../diff1",
                      "/synthetic-storage/overlay/l/", "/synthetic-storage/overlay/l/LINK/diff",
                      "/synthetic-storage-other/overlay/l/LINK", overlay["UpperDir"]):
            with self.subTest(lower=lower):
                self.reject(text.replace("lowerdir=" + overlay["LowerDir"], "lowerdir=" + lower),
                            "unrecognized effective overlay lower layers|aliases writable upper")
        for old, new in (("redirect_dir=nofollow", "redirect_dir=follow"),
                         ("uuid=on", "uuid=off"), (",userxattr", ""),
                         (",userxattr", ",userxattr,unknown=/home")):
            self.reject(text.replace(old, new), "unrecognized overlay superblock options")

    def test_unknown_socket_escaped_and_broad_targets(self):
        text = self.canonical()
        for target in ("/home", "/var/lib/dpkg", "/run", "/run/podman.sock", "/sys/other"):
            with self.subTest(target=target):
                self.reject(text + f"9999 789 0:99 / {target} rw - tmpfs tmpfs rw\n",
                            "host share or unknown mount|socket or escaped mount reference")
        self.reject(text.replace("/etc/hosts", r"/etc/hosts\040"), "socket or escaped mount reference")
        self.reject(text +
                    "9999 789 0:99 / /other rw - tmpfs /run/socket rw\n",
                    "socket or escaped mount reference")

    def test_duplicates_parent_topology_and_malformed_records(self):
        text = self.canonical()
        self.reject(text + text.splitlines()[0] + "\n", "duplicate mount target or ID")
        self.reject(text.replace("743 895", "742 895"), "duplicate mount target or ID")
        self.reject(text.replace("743 895", "743 790"), "unrecognized mount parent: /dev/shm")
        self.reject(text.replace("789 537", "789 790"), "root mount parent is inside")
        self.reject(text.replace("742 789", "742 789x"), "invalid mount identity")
        self.reject(text.replace("rw,nosuid,nodev,relatime", "rw,ro,nosuid,nodev,relatime", 1),
                    "contradictory mount access flags")
        self.reject(text.replace("rw,nosuid,nodev,relatime", "rw,rw,nosuid,nodev,relatime", 1),
                    "empty or duplicate mount option")
        self.reject(text.replace(" - tmpfs", " shared:1 - tmpfs", 1), "unrecognized mountinfo optional fields")
        self.reject(text.replace(" - tmpfs", " tmpfs", 1), "malformed mountinfo")
        self.reject(text.replace("mode=700", "mode=/home", 1),
                    "generated mount superblock is not UID-bound")
        self.reject(text.replace("rw,nosuid,nodev,relatime", "rw,bind,nosuid,nodev,relatime", 1),
                    "unrecognized effective mount flags")

    def test_root_tuple_flags_and_inspect_lower_constraints(self):
        text = self.canonical()
        for old, new, message in (
            ("/ / rw,relatime - overlay overlay", "/home / rw,relatime - overlay overlay",
             "unrecognized root mount"),
            ("- overlay overlay", "- tmpfs overlay", "unrecognized root mount"),
            ("- overlay overlay", "- overlay /home", "unrecognized root mount"),
            ("/ / rw,relatime - overlay", "/ / ro,relatime - overlay", "unrecognized root mount"),
            ("upperdir=", "upperdir=/run/podman.sock", "socket or escaped mount reference"),
        ):
            with self.subTest(new=new):
                self.reject(text.replace(old, new), message)
        overlay = self.inspect["GraphDriver"]["Data"]
        for lower, message in (
            (overlay["LowerDir"] + ":" + overlay["LowerDir"].split(":")[0],
             "unrecognized inspected overlay lower layers"),
            (overlay["UpperDir"], "overlay lower layer aliases writable upper"),
            ("/synthetic-storage/overlay/l/LINK/../../diff1", "unrecognized inspected overlay lower layers"),
        ):
            data = json.loads(json.dumps(self.inspect))
            data["GraphDriver"]["Data"]["LowerDir"] = lower
            with self.subTest(lower=lower):
                self.reject(text, message, running_inspect=data)

    def test_input_bounds_and_invalid_cid(self):
        for text, message in (("", "missing or oversized mountinfo"),
                              ("x" * 131073, "missing or oversized mountinfo"),
                              ("x\n" * 129, "too many mountinfo records")):
            self.reject(text, message)
        with self.assertRaisesRegex(MountValidationError, "invalid container ID"):
            validate_mountinfo(self.text, "../home")


if __name__ == "__main__":
    unittest.main()
