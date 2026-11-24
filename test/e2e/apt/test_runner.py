"""Offline safety tests: no Podman, package tools, or cdots execution."""
import copy
import json
import os
import subprocess
import sys
import unittest
from unittest.mock import Mock, patch

import runner

CID = "a" * 64
TOKEN = "b" * 32
NAME = "cdots-apt-" + TOKEN

# Install before any tests. Exact harmless scripts, not arbitrary Python -c.
PYTHON_FIXTURES = frozenset((sys.executable, "-c", script) for script in (
    'import os,sys; assert all(os.isatty(i) for i in (0,1,2)); print("ready",flush=True)',
    "import time; time.sleep(5)",
    "import os; os.write(1,b'x'*100000)",
    "import os,time; os.write(2,b'x'*100); time.sleep(5)",
))
_REAL_POPEN = runner.subprocess.Popen


def guarded_popen(argv, *args, **kwargs):
    if (not isinstance(argv, (list, tuple)) or tuple(argv) not in PYTHON_FIXTURES
            or args or kwargs.get("shell") or kwargs.get("executable")
            or kwargs.get("preexec_fn")):
        raise AssertionError("offline spawn refused: " + repr(argv))
    return _REAL_POPEN(argv, **kwargs)


_PROCESS_GUARD = patch.object(runner.subprocess, "Popen", side_effect=guarded_popen)
_PROCESS_GUARD.start()


def tearDownModule():
    _PROCESS_GUARD.stop()


def timeout_executor(argv, **kwargs):
    if argv != ["offline-fixture"]:
        raise AssertionError("unexpected timeout fixture argv: " + repr(argv))
    raise runner.CaptureFailure("timeout", b"", b"", None)


class EvidenceRetention(unittest.TestCase):
    def test_evidence_is_private_and_retained_inside_worktree(self):
        root = runner.Path(runner.__file__).resolve().parent / "evidence"
        metadata = Mock(st_uid=os.getuid(), st_mode=0o40700)
        with patch.object(runner.Path, "mkdir") as mkdir, \
                patch.object(runner.Path, "is_symlink", return_value=False), \
                patch.object(runner.Path, "stat", return_value=metadata), \
                patch.object(runner.tempfile, "mkdtemp", return_value=str(root / "run")) as allocate, \
                patch.object(runner.os, "chmod") as chmod, patch("sys.stdout"):
            result = runner.private_evidence()
        self.assertEqual(result, root / "run")
        mkdir.assert_called_once_with(mode=0o700, exist_ok=True)
        allocate.assert_called_once_with(prefix="cdots-phase5a-", dir=root)
        chmod.assert_called_once_with(root / "run", 0o700)


class ProcessBoundary(unittest.TestCase):
    def test_global_guard_blocks_external_families_before_underlying_spawn(self):
        with patch(__name__ + "._REAL_POPEN") as underlying:
            for argv in (["/usr/bin/podman", "--remote=false", "logs", CID],
                         ["/usr/bin/podman", "--remote=false", "stop", "--time=2", CID],
                         ["/usr/bin/go", "build"], ["/usr/bin/apt-get", "update"],
                         ["/tmp/cdots", "apply", "git"], ["offline-fixture"],
                         [sys.executable, "-c", "print('not enumerated')"]):
                with self.subTest(argv=argv), self.assertRaises(AssertionError):
                    runner.subprocess.Popen(argv)
            underlying.assert_not_called()

    def test_guard_accepts_only_exact_fixture_without_executable_override(self):
        fixture = next(iter(PYTHON_FIXTURES))
        with patch(__name__ + "._REAL_POPEN") as underlying:
            runner.subprocess.Popen(list(fixture))
            underlying.assert_called_once_with(list(fixture))
            underlying.reset_mock()
            for kwargs in ({"shell": True}, {"executable": "/usr/bin/podman"}):
                with self.assertRaises(AssertionError):
                    runner.subprocess.Popen(list(fixture), **kwargs)
            underlying.assert_not_called()


def container():
    return {
        "Id": CID, "Name": NAME, "Image": runner.IMAGE,
        "Config": {"Labels": {runner.LABEL: TOKEN}, "User": "0:0",
                   "Volumes": None, "Env": [runner.IMAGE_PATH],
                   "Entrypoint": None, "Cmd": ["/bin/sleep", "infinity"]},
        "Mounts": [], "HostConfig": {
            "Privileged": False, "NetworkMode": "slirp4netns",
            "PidMode": "private", "IpcMode": "private", "UTSMode": "private",
            "CgroupnsMode": "private", "UsernsMode": "auto:size=65536",
            "SecurityOpt": ["no-new-privileges"], "CapAdd": [], "CapDrop": [],
            "Binds": [], "VolumesFrom": [], "Devices": [], "DeviceRequests": [],
            "PortBindings": {}, "PublishAllPorts": False,
            "ReadonlyRootfs": False,
        },
        "EffectiveCaps": ["CAP_CHOWN"], "BoundingCaps": ["CAP_CHOWN"],
    }


FIXTURE = runner.Path(__file__).parent / "fixtures/podman-4.9.3-created.json"
EVIDENCE = runner.Path("/synthetic-evidence")
PARENT_MAP = "0 1000 1\n1 100000 65536\n"


def observed_container():
    with FIXTURE.open(encoding="utf-8") as stream:
        return json.load(stream)


class PodmanCompatibility(unittest.TestCase):
    def test_actual_sanitized_created_inspect(self):
        data = observed_container()
        runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)
        translated = runner.validate_created_maps(data, [PARENT_MAP, PARENT_MAP], 1000, 1000)
        self.assertEqual(translated, {"UidMap": "0 100000 65536", "GidMap": "0 100000 65536"})

    def test_observed_consequential_field_variants_rejected(self):
        cases = {
            "Privileged": True, "CgroupMode": "host", "CgroupManager": "none",
            "Cgroups": "disabled", "Cgroup": "host", "CgroupConf": {"cpu": "1"},
            "NetworkMode": "host", "UsernsMode": "host", "Runtime": "unconfined",
            "PidsLimit": -1, "ShmSize": 1, "CgroupParent": "/system.slice",
            "ConsoleSize": [1, 1], "RestartPolicy": {"Name": "always", "MaximumRetryCount": 0},
            "LogConfig": {"Type": "journald", "Path": "/host/log", "Config": None, "Size": "0B", "Tag": ""},
            "ContainerIDFile": "/not-owned/container.cid", "VolumeDriver": "local",
            "Binds": ["/var/lib/dpkg:/var/lib/dpkg"], "Devices": [{"PathOnHost": "/dev/sda"}],
            "SecurityOpt": ["no-new-privileges", "seccomp=unconfined"],
            "Unknown": True,
        }
        for key, value in cases.items():
            data = observed_container()
            data["HostConfig"][key] = value
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)
        for key, value in (("HealthcheckOnFailureAction", "restart"), ("Passwd", False),
                           ("Umask", "0000"), ("sdNotifyMode", "ignore"),
                           ("Image", "other"), ("Annotations", {"unknown": "value"}),
                           ("Env", [runner.IMAGE_PATH, "container=podman", "APT_CONFIG=/tmp/x"])):
            data = observed_container()
            data["Config"][key] = value
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)

    def test_string_maps_translate_and_reject_aliases_malformed_and_overflow(self):
        for bad in ("0:0:65536", "1:1:65536", "0:1:0", "0:1:65537", "0:4294967295:65536",
                    "0:-1:65536", "0:+1:65536", "00:1:65536", "0:1:65536\n",
                    "0:1:65536:1", "0:" + "1" * 50 + ":65536"):
            data = observed_container()
            data["HostConfig"]["IDMappings"]["UidMap"] = [bad]
            with self.subTest(bad=bad), self.assertRaises(runner.Refusal):
                runner.validate_created_maps(data, [PARENT_MAP, PARENT_MAP], 1000, 1000)
        for parent in ("0 0 1\n1 100000 65536\n", "0 1000 1\n1 0 65536\n",
                       "0 1000 1\n1 999 65536\n", PARENT_MAP + "1 200000 65536\n",
                       "0 1000 1\n1 100000 65536\n65537 100001 65536\n"):
            with self.subTest(parent=parent), self.assertRaises(runner.Refusal):
                runner.validate_created_maps(observed_container(), [parent, parent], 1000, 1000)

    def test_observed_identity_annotations_network_and_type_checks(self):
        for key, value in (("ImageDigest", "sha256:" + "f" * 64), ("Namespace", "host"),
                           ("AppArmorProfile", "unconfined"), ("BoundingCaps", ["CAP_SYS_ADMIN"]),
                           ("Mounts", [{"Source": "/run/podman.sock"}])):
            data = observed_container()
            data[key] = value
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)
        for field in ("Annotations", "CreateCommand", "Env"):
            data = observed_container()
            if field == "Annotations":
                data["Config"][field]["io.podman.annotations.cid-file"] = "/other/container.cid"
            elif field == "CreateCommand":
                data["Config"][field].insert(4, "--privileged")
            else:
                data["Config"][field].append("CONTAINER_HOST=unix:///run/podman.sock")
            with self.subTest(field=field), self.assertRaises(runner.Refusal):
                runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)
        for key, value in (("Privileged", 0), ("CpuCount", False), ("UsernsMode", "private")):
            data = observed_container()
            data["HostConfig"][key] = value
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)
        for key in ("Ports", "SandboxKey", "Networks"):
            data = observed_container()
            data["NetworkSettings"][key] = {"host": {}} if key != "SandboxKey" else "/proc/1/ns/net"
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)
        data = observed_container()
        del data["HostConfig"]["IDMappings"]
        with self.assertRaises(runner.Refusal):
            runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)

    def test_explicit_create_policy_and_mapping_formats(self):
        command = runner.create_command(EVIDENCE, NAME, TOKEN)
        self.assertIn("--network=slirp4netns:allow_host_loopback=false", command)
        self.assertIn("--http-proxy=false", command)
        self.assertIn("--hooks-dir=/synthetic-evidence/empty-hooks", command)
        data = observed_container()
        data["Config"]["CreateCommand"] = command
        runner.validate_container(data, CID, NAME, TOKEN, EVIDENCE)
        data["HostConfig"]["IDMappings"]["UidMap"] = [{"ContainerID": 0, "HostID": 1, "Size": 65536}]
        with self.assertRaises(runner.Refusal):
            runner.validate_created_maps(data, [PARENT_MAP, PARENT_MAP], 1000, 1000)
        data = observed_container()
        data["HostConfig"]["IDMappings"]["GidMap"].append("0:1:65536")
        with self.assertRaises(runner.Refusal):
            runner.validate_created_maps(data, [PARENT_MAP, PARENT_MAP], 1000, 1000)

    def test_observed_running_maps_use_parent_relative_coordinates(self):
        data = observed_container()
        probes = {"guest-uid_map": "0 1 65536", "guest-gid_map": "0 1 65536",
                  "host-uid_map": "0 100000 65536", "host-gid_map": "0 100000 65536"}
        runner.validate_running_maps(data, [PARENT_MAP, PARENT_MAP], probes, 1000, 1000)
        self.assertNotEqual(runner.map_rows(probes["guest-uid_map"]),
                            runner.map_rows(probes["host-uid_map"]))
        for key in probes:
            for bad in ("0 0 65536", "0 1000 65536", "0 200000 65536", "1 1 65536",
                        "0 1 65535", "0 1 65537", "0 4294967295 65536",
                        "0 1 65536\n0 2 65536", "0 -1 65536", "bad"):
                altered = dict(probes, **{key: bad})
                with self.subTest(key=key, bad=bad), self.assertRaises(runner.Refusal):
                    runner.validate_running_maps(data, [PARENT_MAP, PARENT_MAP], altered, 1000, 1000)
        for parent in ("0 1000 1\n1 200000 65536\n",
                       "0 1000 1\n1 999 65536\n", PARENT_MAP + "1 200000 65536\n",
                       "0 1000 1\n1 4294967295 65536\n"):
            with self.subTest(parent=parent), self.assertRaises(runner.Refusal):
                runner.validate_running_maps(data, [parent, parent], probes, 1000, 1000)

    def test_fixture_has_no_runtime_session_coordinates(self):
        text = json.dumps(observed_container())
        for marker in ("/home/", "/run/user/", "cdots-phase5a-", "8347e325", "c47ec0f22d19"):
            self.assertNotIn(marker, text)


class Gates(unittest.TestCase):
    def test_default_never_calls_external_command(self):
        with patch.object(runner, "run_disposable") as live:
            self.assertEqual(runner.main([]), 2)
            live.assert_not_called()

    def test_host_uid_and_remote_environment(self):
        runner.validate_host(1000, {})
        for env in ({"CONTAINER_HOST": "unix:///x"},
                    {"CONTAINER_CONNECTION": "x"},
                    {"CONTAINERS_CONF": "x"}, {"DOCKER_HOST": "x"},
                    {"CONTAINERS_CONF_OVERRIDE": ""}, {"LD_PRELOAD": "x"}):
            with self.subTest(env=env), self.assertRaises(runner.Refusal):
                runner.validate_host(1000, env)
        with self.assertRaises(runner.Refusal):
            runner.validate_host(0, {})

    def test_runtime_requires_local_rootless(self):
        for rootless, remote in ((False, False), (True, True), (None, False)):
            with self.subTest(rootless=rootless, remote=remote):
                with self.assertRaises(runner.Refusal):
                    runner.validate_runtime({"host": {"security": {"rootless": rootless},
                                                       "serviceIsRemote": remote}})

    def test_image_rejects_entrypoint_volumes_and_wrong_pin(self):
        image = {"Id": runner.IMAGE, "Digest": runner.DIGEST,
                 "Os": "linux", "Architecture": "amd64",
                 "Config": {"Env": [runner.IMAGE_PATH], "Cmd": ["bash"]}, "User": ""}
        runner.validate_image(image)
        for key, value in (("Id", CID), ("Digest", "sha256:bad"),
                           ("Architecture", "arm64"), ("User", "1000")):
            bad = copy.deepcopy(image)
            bad[key] = value
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_image(bad)
        for key, value in (("Entrypoint", ["bad"]), ("Volumes", {"/home": {}}),
                           ("Unknown", True)):
            bad = copy.deepcopy(image)
            bad["Config"][key] = value
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_image(bad)

    def test_container_accepts_narrow_fixture(self):
        runner.validate_container(container(), CID, NAME, TOKEN)

    def test_container_hostile_fields(self):
        cases = {
            "Privileged": True, "NetworkMode": "host", "PidMode": "host",
            "IpcMode": "host", "UTSMode": "host", "CgroupnsMode": "host",
            "UsernsMode": "host", "Binds": ["/home:/home"],
            "Devices": [{"PathOnHost": "/dev/sda"}], "VolumesFrom": ["other"],
            "PortBindings": {"80/tcp": [{}]}, "PublishAllPorts": True,
            "SecurityOpt": ["seccomp=unconfined"], "CapAdd": ["SYS_ADMIN"],
            "NewDangerousOption": True,
        }
        for key, value in cases.items():
            bad = container()
            bad["HostConfig"][key] = value
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_container(bad, CID, NAME, TOKEN)
        for key, value in (("Id", "short"), ("Name", "other"),
                           ("Image", "c" * 64), ("Mounts", [{"Source": "/run/podman.sock"}]),
                           ("AppArmorProfile", "unconfined"), ("Unknown", True),
                           ("Rootfs", "/"), ("Pod", "other")):
            bad = container()
            bad[key] = value
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_container(bad, CID, NAME, TOKEN)
        bad = container()
        bad["Config"]["Labels"] = {runner.LABEL: "other"}
        with self.assertRaises(runner.Refusal):
            runner.validate_container(bad, CID, NAME, TOKEN)

    def test_uid_gid_mapping_rejections(self):
        runner.validate_map("0 100000 65536\n", 1000)
        for text in ("0 0 65536", "0 1000 1", "1 100000 65536", "0 999 2",
                     "0 100000 0", "bad", "0 100000 65536\n0 200000 1"):
            with self.subTest(text=text), self.assertRaises(runner.Refusal):
                runner.validate_map(text, 1000)

    def test_namespace_and_mount_checks(self):
        host = {n: n + ":[1]" for n in runner.NAMESPACES}
        guest = {n: n + ":[2]" for n in runner.NAMESPACES}
        runner.validate_namespaces(host, guest)
        guest["user"] = host["user"]
        with self.assertRaises(runner.Refusal):
            runner.validate_namespaces(host, guest)
        runner.validate_mountinfo("1 0 0:1 / / rw - overlay overlay rw\n", CID)
        for text in ("1 0 0:1 /home /home rw - ext4 /dev/sda rw\n",
                     "1 0 0:1 /run/podman.sock /etc/hosts rw - ext4 /dev/sda rw\n",
                     "malformed"):
            with self.subTest(text=text), self.assertRaises(runner.Refusal):
                runner.validate_mountinfo(text, CID)


class Prompts(unittest.TestCase):
    def test_exact_separate_confirmation(self):
        driver = runner.Approvals("install")
        self.assertEqual(driver.feed(runner.FINAL_PROMPT.encode()), b"apply\n")
        self.assertEqual(driver.feed(runner.NATIVE_PROMPT.encode()), b"y\n")
        driver.finish()

    def test_never_approves_unexpected_or_reordered_prompt(self):
        for text in (runner.NATIVE_PROMPT, 'Type "review upgrade bash:amd64" to approve (anything else cancels): ',
                     'Type "remove bash:amd64" to approve (anything else cancels): ',
                     "Do you want to continue? [y/N]", "Password: "):
            with self.subTest(text=text), self.assertRaises(runner.Refusal):
                runner.Approvals("install").feed(text.encode())

    def test_missing_prompt_is_not_fabricated_success(self):
        driver = runner.Approvals("install")
        with self.assertRaises(runner.Refusal):
            driver.finish()
        driver.feed(b"Apply result: failure\nExecution attempted: false\nDiagnostic: unexpected production error\n")
        with self.assertRaises(runner.Refusal):
            driver.finish()
        self.assertIsNone(driver.unsupported_reason)
        self.assertEqual(driver.step, 0)

    def test_fragmented_prompt_and_no_generic_yes_loop(self):
        driver = runner.Approvals("wrong-approval")
        self.assertEqual(driver.feed(runner.FINAL_PROMPT[:20].encode()), b"")
        self.assertEqual(driver.feed(runner.FINAL_PROMPT[20:].encode()), b"deny\n")
        with self.assertRaises(runner.Refusal):
            driver.feed(runner.NATIVE_PROMPT.encode())

    def test_real_three_tty_stream_fixture(self):
        code = ('import os,sys; assert all(os.isatty(i) for i in (0,1,2)); '
                'print("ready",flush=True)')
        rc, text = runner.pty_command([sys.executable, "-c", code],
                                      runner.Approvals("no-op"), 5, Mock(), Mock())
        self.assertEqual(rc, 0)
        self.assertIn(b"ready", text)

    def test_timeout_stops_owned_container_and_marks_uncertain(self):
        stop, record = Mock(), Mock()
        with self.assertRaises(runner.Refusal):
            runner.pty_command([sys.executable, "-c", "import time; time.sleep(5)"],
                               runner.Approvals("no-op"), 0.05, stop, record)
        stop.assert_called_once_with()
        self.assertIn("uncertain", str(record.call_args))

    def test_log_failure_still_stops_owned_container(self):
        stop = Mock()
        record = Mock(side_effect=OSError("evidence disk unavailable"))
        with self.assertRaises((runner.Refusal, OSError)):
            runner.pty_command([sys.executable, "-c", "import time; time.sleep(5)"],
                               runner.Approvals("no-op"), 0.05, stop, record)
        stop.assert_called_once_with()

    def test_eof_fixture_only_answers_expected_application_prompt(self):
        driver = runner.Approvals("eof")
        self.assertEqual(driver.feed(runner.FINAL_PROMPT.encode()), b"\x04")
        driver.finish()


class Lifecycle(unittest.TestCase):
    def bare_run(self):
        unit = runner.Run.__new__(runner.Run)
        unit.cid, unit.name, unit.token = CID, NAME, TOKEN
        unit.verified, unit.uncertain = False, False
        unit.timeout = 1
        unit.env = {}
        unit.base = ["/usr/bin/podman", "--remote=false"]
        unit.inspect = Mock(return_value=container())
        unit.record = Mock()
        unit.save = Mock()
        unit.evidence_errors = []
        unit.executed = []
        permitted = [unit.base + args for args in (
            ["container", "inspect", CID], ["logs", CID],
            ["stop", "--time=2", CID], ["rm", CID])]
        def execute(argv, **kwargs):
            if argv not in permitted:
                raise AssertionError("unexpected synthetic argv: " + repr(argv))
            unit.executed.append(argv)
            output = json.dumps([unit.inspect.return_value]).encode() if "inspect" in argv else b""
            return subprocess.CompletedProcess(argv, 0, output, b"")
        unit.executor = execute
        return unit

    def test_created_maps_translate_rootless_parent_and_reject_host_uid(self):
        data = container()
        data["HostConfig"]["IDMappings"] = {
            "UidMap": [{"ContainerID": 0, "HostID": 1, "Size": 65536}],
            "GidMap": [{"ContainerID": 0, "HostID": 1, "Size": 65536}]}
        parent = "0 1000 1\n1 100000 65536\n"
        runner.validate_created_maps(data, [parent, parent], 1000, 1000)
        for key in ("UidMap", "GidMap"):
            bad = copy.deepcopy(data)
            bad["HostConfig"]["IDMappings"][key][0]["HostID"] = 0
            with self.subTest(key=key), self.assertRaises(runner.Refusal):
                runner.validate_created_maps(bad, [parent, parent], 1000, 1000)
        with self.assertRaises(runner.Refusal):
            runner.validate_created_maps(container(), [parent, parent], 1000, 1000)

    def test_exec_before_probes_is_refused(self):
        unit = self.bare_run()
        unit.podman = Mock()
        with self.assertRaises(runner.Refusal):
            unit.exec("/usr/bin/apt-get", "update")
        unit.podman.assert_not_called()

    def test_cleanup_rejects_mismatched_labels_without_stop_or_remove(self):
        unit = self.bare_run()
        data = container()
        data["Config"]["Labels"] = {runner.LABEL: "not-ours"}
        unit.inspect.return_value = data
        unit.podman = Mock()
        with self.assertRaises(runner.Refusal):
            unit.cleanup()
        unit.podman.assert_not_called()

    def test_cleanup_retains_logs_then_exact_id_and_revalidates(self):
        unit = self.bare_run()
        data = container()
        data["State"] = {"Running": False}
        unit.inspect.return_value = data
        unit.cleanup()
        self.assertEqual(unit.executed, [unit.base + args for args in (
            ["logs", CID], ["stop", "--time=2", CID], ["rm", CID])])
        self.assertEqual(unit.inspect.call_count, 3)
        self.assertTrue(unit.stopped_verified)

    def test_command_timeout_log_failure_still_stops(self):
        unit = self.bare_run()
        unit.stop = Mock()
        unit.record.side_effect = OSError("log failure")
        unit.executor = timeout_executor
        with self.assertRaises(runner.Refusal):
            unit.command(["offline-fixture"], container_command=True)
        unit.stop.assert_called_once_with()

    def test_command_timeout_stops_exact_owned_container(self):
        unit = self.bare_run()
        unit.stop = Mock()
        unit.executor = timeout_executor
        with self.assertRaises(runner.Refusal):
            unit.command(["offline-fixture"], container_command=True)
        unit.stop.assert_called_once_with()
        self.assertTrue(unit.uncertain)
        self.assertIn("uncertain-timeout", str(unit.record.call_args))


class ReviewRegressions(unittest.TestCase):
    def unit(self, fail_at=None, trusted=True, refuses_stop=False):
        unit = runner.Run.__new__(runner.Run)
        unit.cid, unit.name, unit.token = CID, NAME, TOKEN
        unit.verified, unit.uncertain, unit.sequence = True, False, 0
        unit.timeout, unit.env = 1, {}
        unit.base = ["/usr/bin/podman", "--remote=false"]
        unit.evidence_errors = []
        unit.evidence = runner.Path("/synthetic-evidence")
        calls, saved = [], {}
        state = {"running": True}

        def execute(argv, **kwargs):
            permitted = [unit.base + args for args in (
                ["container", "inspect", CID], ["stop", "--time=2", CID],
                ["logs", CID], ["rm", CID])]
            if argv not in permitted:
                raise AssertionError("unexpected synthetic argv: " + repr(argv))
            calls.append(argv)
            args = argv[len(unit.base):]
            if args == ["container", "inspect", CID]:
                data = container()
                data["State"] = {"Running": state["running"], "Pid": 12345}
                if not trusted:
                    data["Config"]["Labels"] = {runner.LABEL: "someone-else"}
                output = json.dumps([data]).encode()
            elif args == ["stop", "--time=2", CID]:
                if not refuses_stop:
                    state["running"] = False
                output = b""
            elif args in (["logs", CID], ["rm", CID]):
                output = b""
            else:
                self.fail("unexpected injected command: " + str(argv))
            return subprocess.CompletedProcess(argv, 0, output, b"")

        def save(name, value):
            command = value.get("argv", [])
            matches = (fail_at == "all" or name == fail_at or
                       (fail_at == "inspect-record" and "inspect" in command) or
                       (fail_at == "logs-record" and "logs" in command) or
                       (fail_at == "stop-record" and "stop" in command))
            if matches:
                raise OSError("injected storage failure: " + str(fail_at))
            saved[name] = value
        unit.executor, unit.save = execute, save
        return unit, calls, saved

    def assert_stopped(self, unit, calls):
        self.assertIn(unit.base + ["stop", "--time=2", CID], calls)
        stop_index = calls.index(unit.base + ["stop", "--time=2", CID])
        self.assertIn(unit.base + ["container", "inspect", CID], calls[stop_index + 1:])
        self.assertTrue(unit.stopped_verified)

    def test_real_stop_survives_inspect_and_stop_record_failures(self):
        for step in ("inspect-record", "stop-record", "all"):
            with self.subTest(step=step):
                unit, calls, _ = self.unit(step)
                unit.stop()
                self.assert_stopped(unit, calls)
                self.assertTrue(unit.uncertain)
                self.assertTrue(unit.evidence_errors)

    def test_real_cleanup_stops_despite_each_storage_failure(self):
        for step in ("inspect-record", "final-inspect.json", "logs-record", "stop-record"):
            with self.subTest(step=step):
                unit, calls, _ = self.unit(step)
                with self.assertRaises(runner.Refusal):
                    unit.cleanup()
                self.assert_stopped(unit, calls)
                self.assertNotIn(unit.base + ["rm", CID], calls)

    def test_real_cleanup_and_stop_never_target_untrusted_identity(self):
        for method in ("stop", "cleanup"):
            with self.subTest(method=method):
                unit, calls, _ = self.unit("all", trusted=False)
                with self.assertRaises(runner.Refusal):
                    getattr(unit, method)()
                self.assertFalse(any("stop" in argv or "rm" in argv for argv in calls))

    def test_real_cleanup_never_removes_running_object(self):
        unit, calls, _ = self.unit(refuses_stop=True)
        with self.assertRaises(runner.Refusal):
            unit.cleanup()
        self.assertNotIn(unit.base + ["rm", CID], calls)

    def test_real_cleanup_verifies_stopped_before_remove(self):
        unit, calls, _ = self.unit()
        unit.cleanup()
        self.assert_stopped(unit, calls)
        self.assertEqual(calls[-1], unit.base + ["rm", CID])

    def test_real_pty_stop_callback_survives_storage_failure(self):
        unit, calls, _ = self.unit("all")
        with self.assertRaises((OSError, runner.Refusal)):
            runner.pty_command([sys.executable, "-c", "import time; time.sleep(5)"],
                               runner.Approvals("no-op"), 0.05, unit.stop, unit.record)
        self.assert_stopped(unit, calls)
        self.assertTrue(unit.evidence_errors)

    def test_cleanup_log_capture_overflow_still_stops_without_removal(self):
        unit, calls, _ = self.unit()
        fake = unit.executor
        def execute(argv, **kwargs):
            if argv == unit.base + ["logs", CID]:
                raise runner.CaptureFailure("overflow", b"bounded", b"", None)
            return fake(argv, **kwargs)
        unit.executor = execute
        with self.assertRaises(runner.CaptureFailure):
            unit.cleanup()
        self.assert_stopped(unit, calls)
        self.assertNotIn(unit.base + ["rm", CID], calls)

    def test_unknown_or_duplicate_result_records_are_rejected(self):
        for transcript in (b"Apply result: failure\nApply result: unexpected\nExecution attempted: false\n",
                           b"Apply result: failure\nExecution attempted: false\nExecution attempted: unknown\n"):
            with self.subTest(transcript=transcript), self.assertRaises(runner.Refusal):
                runner.apply_records(transcript)

    def test_host_hash_traversal_error_is_not_empty_success(self):
        def inaccessible(root, **kwargs):
            if "onerror" in kwargs:
                kwargs["onerror"](PermissionError("unreadable subtree"))
            return iter([])
        with patch.object(runner.Path, "is_dir", return_value=True), \
                patch.object(os, "walk", side_effect=inaccessible):
            with self.assertRaises((OSError, runner.Refusal)):
                runner.host_hashes()

    def test_host_hash_disappearing_directory_inventory_is_error(self):
        with patch.object(runner.Path, "is_dir", return_value=True), \
                patch.object(os, "walk", return_value=iter([("/var/lib/dpkg", ["gone"], [])])), \
                patch.object(os, "lstat", side_effect=FileNotFoundError("gone")):
            with self.assertRaises(OSError):
                runner.host_hashes()

    def test_host_hash_symlink_inventory_error_is_error(self):
        with patch.object(runner.Path, "is_dir", return_value=True), \
                patch.object(os, "walk", return_value=iter([("/var/lib/dpkg", ["link"], [])])), \
                patch.object(os, "lstat", return_value=os.stat_result((0o120777, 1, 1, 1, 1, 1, 0, 0, 0, 0))), \
                patch.object(os, "readlink", side_effect=PermissionError("link")):
            with self.assertRaises(OSError):
                runner.host_hashes()

    def test_unsupported_requires_enumerated_apt_version_diagnostic(self):
        driver = runner.Approvals("install")
        driver.feed(b"Apply result: failure\nExecution attempted: false\nDiagnostic: guarded execution requires source-established APT 2.6.1\n")
        driver.finish()
        self.assertIsNotNone(driver.unsupported_reason)
        for text in ("Apply result: failure\nApply result: success\nExecution attempted: false\n",
                     "prefix Apply result: failure\nExecution attempted: false\n",
                     "Apply result: failure\nExecution attempted: false\nExecution attempted: true\n"):
            driver = runner.Approvals("install")
            driver.feed(text.encode())
            with self.subTest(text=text), self.assertRaises(runner.Refusal):
                driver.finish()

    def test_package_manifest_has_no_package_filter(self):
        unit, _, saved = self.unit()
        unit.exec = Mock(return_value=(0, b"git\t1\tinstall ok installed\nlibdependency\t1\tinstall ok installed\n"))
        unit.package_state("fixture-before")
        args = unit.exec.call_args.args
        self.assertEqual(len(args), 3)
        self.assertIn("${binary:Package}", args[2])
        self.assertIn("libdependency", saved["fixture-before-packages.json"]["output"])

    def test_bounded_all_command_output_and_timeout_use_real_stop(self):
        for code, expected in (("import os; os.write(1,b'x'*100000)", "overflow"),
                               ("import os,time; os.write(2,b'x'*100); time.sleep(5)", "timeout")):
            with self.subTest(expected=expected):
                unit, calls, saved = self.unit()
                fake = unit.executor
                def execute(argv, **kwargs):
                    if tuple(argv) in PYTHON_FIXTURES:
                        return runner.bounded_command(argv, **kwargs)
                    return fake(argv, **kwargs)
                unit.executor, unit.output_limit, unit.timeout = execute, 1024, 0.15
                with self.assertRaisesRegex(runner.Refusal, expected):
                    unit.command([sys.executable, "-c", code], container_command=True)
                self.assert_stopped(unit, calls)
                values = [v for v in saved.values() if v.get("argv", [None])[0] == sys.executable]
                self.assertTrue(values)
                self.assertLessEqual(len(values[0]["stdout"]) + len(values[0]["stderr"]), 1024)

    def probe_unit(self, incompatible=False):
        unit, calls, saved = self.unit()
        unit.base += ["--hooks-dir=" + str(unit.evidence / "empty-hooks")]
        fallback = unit.executor
        image = {"Id": runner.IMAGE, "Digest": runner.DIGEST,
                 "Os": "linux", "Architecture": "amd64", "User": "",
                 "Config": {"Env": [runner.IMAGE_PATH], "Cmd": ["bash"]}}
        permitted = [unit.base + args for args in (
            ["container", "inspect", CID], ["logs", CID], ["stop", "--time=2", CID], ["rm", CID],
            ["info", "--format=json"], ["image", "inspect", runner.IMAGE], ["start", CID],
            ["unshare", "/bin/cat", "/proc/self/uid_map"],
            ["unshare", "/bin/cat", "/proc/self/gid_map"],
            ["create", "--pull=never", "--http-proxy=false", "--name=" + NAME,
             "--cidfile=" + str(unit.evidence / "container.cid"), "--label=" + runner.LABEL + "=" + TOKEN,
             "--user=0:0", "--userns=auto:size=65536", "--network=slirp4netns:allow_host_loopback=false", "--pid=private",
             "--ipc=private", "--uts=private", "--cgroupns=private", "--security-opt=no-new-privileges",
             "--entrypoint=", runner.IMAGE, "/bin/sleep", "infinity"],
        )]
        permitted += [unit.base + ["exec", CID, "/usr/bin/readlink", "/proc/self/ns/" + n]
                      for n in runner.NAMESPACES]
        permitted += [unit.base + ["exec", CID, "/bin/cat", p] for p in (
            "/proc/self/uid_map", "/proc/self/gid_map", "/proc/self/status", "/proc/self/mountinfo", "/etc/os-release")]
        permitted += [unit.base + ["exec", CID, "/usr/bin/id", flag] for flag in ("-u", "-g")]
        def execute(argv, **kwargs):
            if argv not in permitted:
                raise AssertionError("unexpected synthetic probe argv: " + repr(argv))
            args = argv[len(unit.base):]
            if args[:2] == ["container", "inspect"]:
                proc = fallback(argv, **kwargs)
                state = json.loads(proc.stdout)[0]["State"]
                data = observed_container()
                data["State"] = state
                data["Config"]["CreateCommand"] = runner.create_command(EVIDENCE, NAME, TOKEN)
                if incompatible:
                    data["HostConfig"]["LogConfig"] = {"Type": "k8s-file"}
                proc.stdout = json.dumps([data]).encode()
                return proc
            if args[:1] in (["logs"], ["stop"], ["rm"]):
                return fallback(argv, **kwargs)
            calls.append(argv)
            if args[:1] == ["info"]:
                out = json.dumps({"host": {"security": {"rootless": True}, "serviceIsRemote": False}})
            elif args[:2] == ["image", "inspect"]:
                out = json.dumps([image])
            elif args[:1] == ["unshare"]:
                out = "0 1000 1\n1 100000 65536\n"
            elif args[:1] == ["create"]:
                self.assertIn("--http-proxy=false", args)
                out = CID
            elif args[:1] == ["start"]:
                out = ""
            elif args[:1] == ["exec"]:
                command, operand = args[2:]
                if command == "/usr/bin/readlink":
                    out = operand.rsplit("/", 1)[1] + ":[2]"
                elif command == "/usr/bin/id":
                    out = "0"
                elif command == "/bin/cat":
                    out = {"/proc/self/uid_map": "0 1 65536\n",
                           "/proc/self/gid_map": "0 1 65536\n",
                           "/proc/self/status": "NoNewPrivs:\t1\nSeccomp:\t2\n",
                           "/proc/self/mountinfo": "1 0 0:1 / / rw - overlay overlay rw\n",
                           "/etc/os-release": 'ID=debian\nVERSION_ID="12"\nVERSION_CODENAME=bookworm\n'}[operand]
                else:
                    self.fail("prohibited probe command " + command)
            else:
                self.fail("prohibited probe family " + str(args))
            return subprocess.CompletedProcess(argv, 0, out.encode(), b"")
        unit.executor = execute
        return unit, calls, saved

    def test_probe_only_real_create_probes_cleanup_excludes_packages_and_build(self):
        for incompatible in (False, True):
            with self.subTest(incompatible=incompatible):
                unit, calls, saved = self.probe_unit(incompatible)
                def initialize(target, evidence, timeout):
                    target.__dict__.update(unit.__dict__)
                with patch.object(runner.Run, "__init__", initialize), \
                        patch.object(runner, "private_evidence", create=True, return_value=unit.evidence), \
                        patch.object(os, "getuid", return_value=1000), \
                        patch.object(os, "geteuid", return_value=1000), \
                        patch.object(os, "getgid", return_value=1000), \
                        patch.object(os.path, "isfile", return_value=True), \
                        patch.object(os, "readlink", side_effect=lambda path: path.rsplit("/", 1)[1] + ":[1]"), \
                        patch.object(runner.Path, "read_text", return_value="0 100000 65536\n"), \
                        patch.dict(os.environ, {}, clear=True):
                    rc = runner.run_probe_only(type("Args", (), {"timeout": 1})())
                self.assertEqual(rc, 1 if incompatible else 0)
                self.assertIn("created-inspect.json", saved)
                self.assertIn("probes.json", saved)
                self.assertFalse(any("apt-get" in arg or "dpkg" in arg or arg in ("/tmp/cdots", "build")
                                     for argv in calls for arg in argv[2:]))
                self.assertFalse(any("cp" in argv or "update" in argv for argv in calls))
                if incompatible:
                    self.assertFalse(any("start" in argv for argv in calls))
                    self.assertEqual(saved["probes.json"]["stage"], "prestart")
                else:
                    self.assertTrue(saved["probes.json"]["complete"])
                    self.assertNotIn("apt_version", saved["probes.json"])
                self.assertIn(unit.base + ["stop", "--time=2", CID], calls)

    def test_created_mode_alone_cannot_authorize_historical_network_declaration(self):
        unit, calls, _ = self.probe_unit()
        fake = unit.executor
        def execute(argv, **kwargs):
            proc = fake(argv, **kwargs)
            if argv == unit.base + ["container", "inspect", CID]:
                data = json.loads(proc.stdout)[0]
                data["Config"]["CreateCommand"] = runner.create_command(EVIDENCE, NAME, TOKEN, "slirp4netns")
                proc.stdout = json.dumps([data]).encode()
            return proc
        unit.executor = execute
        with patch.object(os, "getuid", return_value=1000), patch.object(os, "getgid", return_value=1000):
            with self.assertRaisesRegex(runner.Refusal, "exact issued argv"):
                unit.create(probe_only=True)
        self.assertFalse(any("start" in argv for argv in calls))
        unit.cleanup()
        self.assert_stopped(unit, calls)

    def test_all_readonly_probe_data_survives_guest_map_validation_failure(self):
        unit, calls, saved = self.probe_unit()
        fake = unit.executor
        def execute(argv, **kwargs):
            proc = fake(argv, **kwargs)
            if argv == unit.base + ["exec", CID, "/bin/cat", "/proc/self/uid_map"]:
                proc.stdout = b"0 0 65536\n"
            return proc
        unit.executor = execute
        with patch.object(os, "getuid", return_value=1000), \
                patch.object(os, "getgid", return_value=1000), \
                patch.object(os, "readlink", side_effect=lambda path: path.rsplit("/", 1)[1] + ":[1]"), \
                patch.object(runner.Path, "read_text", return_value="0 100000 65536\n"):
            with self.assertRaisesRegex(runner.Refusal, "mapping disagrees"):
                unit.create(probe_only=True)
        evidence = saved["probes.json"]
        for key in ("status", "mountinfo", "os_release", "host-uid_map", "host-gid_map",
                    "guest-uid_map", "guest-gid_map", "uid", "gid", "host_namespaces", "guest_namespaces"):
            self.assertIn(key, evidence)
        self.assertFalse(evidence["complete"])
        self.assertFalse(unit.verified)
        self.assertNotIn("apt_version", evidence)
        unit.cleanup()
        self.assert_stopped(unit, calls)

    def test_running_mapping_rechecks_fresh_parent_before_authorizing(self):
        unit, calls, saved = self.probe_unit()
        fake = unit.executor
        parent_reads = 0
        def execute(argv, **kwargs):
            nonlocal parent_reads
            proc = fake(argv, **kwargs)
            if argv[len(unit.base):len(unit.base) + 1] == ["unshare"]:
                parent_reads += 1
                if parent_reads > 2:
                    proc.stdout = b"0 1000 1\n1 200000 65536\n"
            return proc
        unit.executor = execute
        with patch.object(os, "getuid", return_value=1000), \
                patch.object(os, "getgid", return_value=1000), \
                patch.object(os, "readlink", side_effect=lambda path: path.rsplit("/", 1)[1] + ":[1]"), \
                patch.object(runner.Path, "read_text", return_value="0 100000 65536\n"):
            with self.assertRaisesRegex(runner.Refusal, "fresh parent composition"):
                unit.create(probe_only=True)
        self.assertEqual(parent_reads, 4)
        self.assertTrue(saved["probes.json"]["collected"])
        self.assertFalse(saved["probes.json"]["complete"])
        self.assertFalse(unit.verified)
        unit.cleanup()
        self.assert_stopped(unit, calls)

    def test_probe_only_direct_package_and_copy_paths_are_refused(self):
        unit, _, _ = self.unit()
        unit.probe_only = True
        for method, args in ((unit.exec, ("/usr/bin/apt-get", "update")),
                             (unit.probe, ("/usr/bin/apt-get", "--version")),
                             (unit.copy, (runner.Path("unused"), "/tmp/unused"))):
            with self.subTest(method=method.__name__), self.assertRaises(runner.Refusal):
                method(*args)

    def test_probe_only_routes_without_apply_path(self):
        with patch.object(runner, "run_disposable") as live, \
                patch.object(runner, "run_probe_only", create=True, return_value=0) as probe:
            self.assertEqual(runner.main(["--probe-only"]), 0)
            probe.assert_called_once()
            live.assert_not_called()
            with self.assertRaises(SystemExit):
                runner.main(["--probe-only", "--run-disposable"])


if __name__ == "__main__":
    unittest.main()
