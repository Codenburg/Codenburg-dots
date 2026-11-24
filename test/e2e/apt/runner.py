#!/usr/bin/env python3
"""Opt-in, fail-closed local rootless Podman APT fixtures. Stdlib only.

This file is not live validation evidence. Unknown isolation schemas are refused.
"""
import argparse
import errno
import hashlib
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import stat
import subprocess
import sys
import tempfile
import time
import uuid

IMAGE = "94926f0fd968cd68ac486502185a5dd78687874a9ec7727f64fe2af696e28311"
DIGEST = "sha256:2c037a04925515fdd6ea85ea14a682d0e79931f5e9f5d07b6dbfc6ba12f9e858"
IMAGE_PATH = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
LABEL = "io.cdots.phase5a.owner"
NAMESPACES = ("user", "mnt", "pid", "net", "ipc", "uts", "cgroup")
FINAL_PROMPT = 'Type "apply" to approve (anything else cancels): '
NATIVE_PROMPT = "Do you want to continue? [Y/n]"
SCENARIOS = ("install", "no-op", "wrong-approval", "eof", "non-tty",
             "native-config", "apt-error", "protected")
FULL_ID = re.compile(r"[0-9a-f]{64}\Z")
OUTPUT_LIMIT = 4 * 1024 * 1024
CAPABILITY_DIAGNOSTICS = {
    "Diagnostic: guarded execution requires source-established APT 2.6.1":
        "source-established APT 2.6.1 required",
}


class Refusal(RuntimeError):
    """Safety evidence is missing, contradictory, or unsupported."""


class CaptureFailure(Refusal):
    def __init__(self, state, stdout, stderr, rc):
        super().__init__("command " + state + "; state uncertain")
        self.state, self.stdout, self.stderr, self.rc = state, stdout, stderr, rc


def bounded_command(argv, *, timeout, env=None, cwd=None, limit=OUTPUT_LIMIT):
    """Drain both pipes incrementally with one combined bound; never communicate()."""
    process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, start_new_session=True, env=env, cwd=cwd)
    outputs = {process.stdout: bytearray(), process.stderr: bytearray()}
    opened = list(outputs)
    deadline, total = time.monotonic() + timeout, 0
    complete = False
    try:
        while opened or process.poll() is None:
            if time.monotonic() >= deadline:
                raise CaptureFailure("timeout", bytes(outputs[process.stdout]),
                                     bytes(outputs[process.stderr]), process.poll())
            ready, _, _ = select.select(opened, [], [], min(0.05, max(0, deadline - time.monotonic())))
            for stream in ready:
                chunk = os.read(stream.fileno(), min(65536, limit - total + 1))
                if not chunk:
                    opened.remove(stream)
                    continue
                remaining = limit - total
                outputs[stream].extend(chunk[:remaining])
                total += min(len(chunk), remaining)
                if len(chunk) > remaining:
                    raise CaptureFailure("overflow", bytes(outputs[process.stdout]),
                                         bytes(outputs[process.stderr]), process.poll())
        rc = process.wait()
        complete = True
        return subprocess.CompletedProcess(argv, rc, bytes(outputs[process.stdout]),
                                           bytes(outputs[process.stderr]))
    finally:
        # On failure also kill the exact local group if a descendant holds a pipe.
        if not complete:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        process.wait(timeout=5)
        process.stdout.close()
        process.stderr.close()


def apply_records(transcript):
    text = transcript.decode("utf-8", "strict").replace("\r\n", "\n")
    results = re.findall(r"^Apply result: (.*)$", text, re.M)
    attempts = re.findall(r"^Execution attempted: (.*)$", text, re.M)
    require(len(results) == len(attempts) == 1, "missing or ambiguous apply result records")
    require(results[0] in {"success", "no-action", "partial", "failure", "cancellation"}
            and attempts[0] in {"true", "false"}, "unknown apply result records")
    return results[0], attempts[0] == "true"


def known_capability(text):
    diagnostics = re.findall(r"^Diagnostic: .+$", text, re.M)
    return CAPABILITY_DIAGNOSTICS.get(diagnostics[0]) if len(diagnostics) == 1 else None


def require(condition, message):
    if not condition:
        raise Refusal(message)


def validate_host(uid, env):
    require(uid > 0, "host root is forbidden")
    forbidden = {"DOCKER_HOST", "DOCKER_CONTEXT", "CONTAINER_HOST",
                 "CONTAINER_CONNECTION", "CONTAINERS_CONF",
                 "CONTAINERS_CONF_OVERRIDE", "CONTAINERS_CONF_MODULES",
                 "CONTAINERS_STORAGE_CONF", "STORAGE_DRIVER", "STORAGE_OPTS",
                 "CONTAINERS_REGISTRIES_CONF", "CONTAINERS_REGISTRIES_CONF_DIR",
                 "CONTAINERS_POLICY", "CONTAINERS_HELPER_BINARY_DIR",
                 "PODMAN_CONNECTIONS_CONF", "LD_PRELOAD", "LD_LIBRARY_PATH"}
    require(not (forbidden & set(env)), "remote/config override environment forbidden")
    require(not any(k.startswith(("PODMAN_", "CONTAINER_", "CONTAINERS_", "STORAGE_"))
                    or k == "XDG_CONFIG_HOME" for k in env), "runtime configuration override forbidden")


def validate_runtime(info):
    host = info.get("host", {})
    require(host.get("security", {}).get("rootless") is True, "rootless evidence required")
    require(host.get("serviceIsRemote") is False, "local runtime evidence required")


def validate_image(image):
    for key, value in {"Id": IMAGE, "Digest": DIGEST, "Os": "linux",
                       "Architecture": "amd64", "User": ""}.items():
        require(image.get(key) == value, "unexpected image " + key)
    config = image.get("Config")
    require(config == {"Env": [IMAGE_PATH], "Cmd": ["bash"]},
            "unrecognized pinned image configuration (including volumes/entrypoint)")


# Safety-relevant objects use closed schemas. Non-operational inspect metadata
# (timestamps, graph paths, counters) is retained in evidence, not interpreted.
HOST_EXACT = {
    "Privileged": False, "NetworkMode": "slirp4netns", "PidMode": "private",
    "IpcMode": "private", "UTSMode": "private", "CgroupnsMode": "private",
    "UsernsMode": "auto:size=65536", "SecurityOpt": ["no-new-privileges"],
    "PublishAllPorts": False, "ReadonlyRootfs": False,
}
HOST_EMPTY = {"Binds", "VolumesFrom", "Devices", "DeviceRequests", "PortBindings",
              "CapAdd", "CapDrop", "ExtraHosts", "Links", "Dns", "DnsOptions",
              "DnsSearch", "GroupAdd", "Ulimits", "Tmpfs", "Sysctls",
              "DeviceCgroupRules", "Annotations", "MaskedPaths", "ReadonlyPaths"}
HOST_DEFAULT = {"LogConfig", "RestartPolicy", "ShmSize", "Runtime", "ConsoleSize",
                "CpuShares", "Memory", "NanoCpus", "CgroupParent", "BlkioWeight",
                "BlkioWeightDevice", "BlkioDeviceReadBps", "BlkioDeviceWriteBps",
                "BlkioDeviceReadIOps", "BlkioDeviceWriteIOps", "CpuPeriod", "CpuQuota",
                "CpuRealtimePeriod", "CpuRealtimeRuntime", "CpusetCpus", "CpusetMems",
                "MemoryReservation", "MemorySwap", "MemorySwappiness", "OomKillDisable",
                "PidsLimit", "OomScoreAdj", "AutoRemove", "Init", "ContainerIDFile"}


# Exact Podman 4.9.3 created-state observations, not permissive default unions.
PODMAN_HOST = {
    "Privileged": False, "NetworkMode": "slirp4netns", "PidMode": "private",
    "IpcMode": "private", "UTSMode": "private", "CgroupMode": "private",
    "CgroupManager": "systemd", "Cgroups": "default", "Cgroup": "", "CgroupConf": None,
    "UsernsMode": "", "SecurityOpt": ["no-new-privileges"], "PublishAllPorts": False,
    "ReadonlyRootfs": False, "AutoRemove": False, "VolumeDriver": "", "VolumesFrom": None,
    "Binds": [], "CapAdd": [], "CapDrop": [], "Devices": [], "PortBindings": {},
    "Dns": [], "DnsOptions": [], "DnsSearch": [], "ExtraHosts": [], "GroupAdd": [],
    "Links": None, "Tmpfs": {}, "Ulimits": [], "Isolation": "",
    "LogConfig": {"Type": "journald", "Config": None, "Path": "", "Tag": "", "Size": "0B"},
    "RestartPolicy": {"Name": "", "MaximumRetryCount": 0}, "Runtime": "oci",
    "PidsLimit": 2048, "ShmSize": 65536000, "CgroupParent": "user.slice", "ConsoleSize": [0, 0],
    "CpuShares": 0, "Memory": 0, "NanoCpus": 0, "BlkioWeight": 0,
    "BlkioWeightDevice": None, "BlkioDeviceReadBps": None, "BlkioDeviceWriteBps": None,
    "BlkioDeviceReadIOps": None, "BlkioDeviceWriteIOps": None, "CpuPeriod": 0, "CpuQuota": 0,
    "CpuRealtimePeriod": 0, "CpuRealtimeRuntime": 0, "CpusetCpus": "", "CpusetMems": "",
    "MemoryReservation": 0, "MemorySwap": 0, "MemorySwappiness": 0, "OomKillDisable": False,
    "OomScoreAdj": 0, "CpuCount": 0, "CpuPercent": 0, "DiskQuota": 0, "KernelMemory": 0,
    "IOMaximumBandwidth": 0, "IOMaximumIOps": 0,
}
PODMAN_CONFIG = {
    "HealthcheckOnFailureAction": "none", "Image": "docker.io/library/debian:bookworm",
    "Passwd": True, "Umask": "0022", "sdNotifyMode": "container",
    "AttachStdin": False, "AttachStdout": False, "AttachStderr": False, "Tty": False,
    "OpenStdin": False, "StdinOnce": False, "Domainname": "", "WorkingDir": "/",
    "StopSignal": 15, "StopTimeout": 10, "Timeout": 0,
}
NETWORK_ARG = "slirp4netns:allow_host_loopback=false"


def exact_value(value, expected):
    if type(value) is not type(expected):
        return False
    if isinstance(expected, dict):
        return set(value) == set(expected) and all(exact_value(value[k], v) for k, v in expected.items())
    if isinstance(expected, list):
        return len(value) == len(expected) and all(exact_value(a, b) for a, b in zip(value, expected))
    return value == expected


def create_command(evidence, name, token, network=NETWORK_ARG):
    evidence = Path(evidence)
    require(evidence.is_absolute() and ".." not in evidence.parts, "absolute owned evidence path required")
    return ["/usr/bin/podman", "--remote=false", "--hooks-dir=" + str(evidence / "empty-hooks"),
            "create", "--pull=never", "--http-proxy=false", "--name=" + name,
            "--cidfile=" + str(evidence / "container.cid"), "--label=" + LABEL + "=" + token,
            "--user=0:0", "--userns=auto:size=65536", "--network=" + network,
            "--pid=private", "--ipc=private", "--uts=private", "--cgroupns=private",
            "--security-opt=no-new-privileges", "--entrypoint=", IMAGE, "/bin/sleep", "infinity"]


def validate_podman_fields(data, cid, name, token, evidence):
    require(evidence is not None, "owned evidence coordinates required for Podman inspect")
    config, host = data["Config"], data["HostConfig"]
    for key, expected in PODMAN_CONFIG.items():
        require(key in config and exact_value(config[key], expected), "unexpected Podman Config " + key)
    require(config.get("Hostname") == cid[:12], "unexpected container hostname")
    require(config.get("Annotations") == {"io.podman.annotations.cid-file": str(Path(evidence) / "container.cid")},
            "cidfile annotation does not identify owned evidence")
    # The fixture retains the historical observed argv. Actual Run.create below
    # additionally requires its exact newly issued argv, including loopback=false.
    require(config.get("CreateCommand") in (create_command(evidence, name, token),
                                            create_command(evidence, name, token, "slirp4netns")),
            "unexpected create declaration")
    expected_host = dict(PODMAN_HOST, ContainerIDFile=str(Path(evidence) / "container.cid"))
    require(set(host) == set(expected_host) | {"IDMappings"}, "unrecognized/missing Podman HostConfig fields")
    for key, expected in expected_host.items():
        require(exact_value(host[key], expected), "unexpected Podman HostConfig " + key)
    # Empty UsernsMode is NOT private-namespace evidence. Require explicit maps;
    # Run.create translates them through fresh parent maps before start, and
    # verify_probes still requires actual distinct namespaces and host UID maps.
    declared_maps(data)
    require(all(isinstance(entries[0], str) for entries in host["IDMappings"].values()),
            "Podman 4.9.3 requires observed string mapping declarations")
    for key, expected in {"ImageDigest": DIGEST, "ImageName": "docker.io/library/debian:bookworm",
                          "OCIRuntime": "crun", "Driver": "overlay", "Namespace": "",
                          "Path": "/bin/sleep", "Args": ["infinity"]}.items():
        require(exact_value(data.get(key), expected), "unexpected Podman " + key)
    require(type(data.get("lockNumber")) is int and 0 <= data["lockNumber"] < 2**31,
            "invalid lock counter")
    network = data.get("NetworkSettings", {})
    require(network.get("Ports") == {} and network.get("Bridge") == "" and
            network.get("SandboxKey") == "", "published ports/host network references forbidden")
    networks = network.get("Networks")
    require(isinstance(networks, dict) and set(networks) == {"slirp4netns"}, "unexpected networks")
    expected_network = {"EndpointID": "", "Gateway": "", "IPAddress": "", "IPPrefixLen": 0,
                        "IPv6Gateway": "", "GlobalIPv6Address": "", "GlobalIPv6PrefixLen": 0,
                        "MacAddress": "", "NetworkID": "slirp4netns", "DriverOpts": None,
                        "IPAMConfig": None, "Links": None}
    expected_settings = {"EndpointID": "", "Gateway": "", "IPAddress": "", "IPPrefixLen": 0,
                         "IPv6Gateway": "", "GlobalIPv6Address": "", "GlobalIPv6PrefixLen": 0,
                         "MacAddress": "", "Bridge": "", "SandboxID": "", "SandboxKey": "",
                         "HairpinMode": False, "LinkLocalIPv6Address": "", "LinkLocalIPv6PrefixLen": 0,
                         "Ports": {}, "Networks": {"slirp4netns": expected_network}}
    require(exact_value(network, expected_settings), "unrecognized slirp network configuration")


def validate_identity(data, cid, name, token):
    require(bool(FULL_ID.fullmatch(cid)), "full container ID required")
    require(data.get("Id") == cid and data.get("Name") == name,
            "container ID/name mismatch")
    require(data.get("Image") == IMAGE, "container image mismatch")
    require(data.get("Config", {}).get("Labels") == {LABEL: token},
            "container ownership labels mismatch")


def validate_container(data, cid, name, token, evidence=None):
    validate_identity(data, cid, name, token)
    observed = "ImageDigest" in data or "CgroupMode" in data.get("HostConfig", {})
    metadata = {"Id", "Created", "Path", "Args", "State", "Image", "ImageName",
                "Name", "Rootfs", "ResolvConfPath", "HostnamePath", "HostsPath",
                "StaticDir", "OCIConfigPath", "ConmonPidFile", "PidFile",
                "LogPath", "RestartCount", "Driver", "MountLabel", "ProcessLabel",
                "AppArmorProfile", "EffectiveCaps", "BoundingCaps", "ExecIDs",
                "GraphDriver", "Mounts", "Dependencies", "NetworkSettings",
                "Namespace", "IsInfra", "IsService", "Config", "HostConfig",
                "OCIRuntime", "Pod", "LockNumber", "KubeExitCodePropagation", "ImageDigest", "lockNumber"}
    require(not (set(data) - metadata), "unrecognized container inspect fields")
    require(not data.get("Rootfs") and not data.get("Pod") and not data.get("Dependencies")
            and not data.get("IsInfra") and not data.get("IsService"),
            "external rootfs/pod/dependencies forbidden")
    require(data.get("AppArmorProfile") != "unconfined", "unconfined AppArmor forbidden")
    require(data.get("Mounts") == [], "mounts forbidden")
    config = data.get("Config", {})
    allowed = {"Labels", "User", "Volumes", "Env", "Entrypoint", "Cmd",
               "Hostname", "Domainname", "AttachStdin", "AttachStdout", "AttachStderr",
               "Tty", "OpenStdin", "StdinOnce", "WorkingDir", "StopSignal",
               "StopTimeout", "Timeout", "OnBuild", "ExposedPorts", "Healthcheck",
               "CreateCommand", "Annotations"}
    if observed:
        allowed |= set(PODMAN_CONFIG)
    require(not (set(config) - allowed), "unrecognized container Config fields")
    require(config.get("User") == "0:0", "only namespaced container root expected")
    require(config.get("Cmd") == ["/bin/sleep", "infinity"], "unexpected container command")
    require(not config.get("Entrypoint") and not config.get("Volumes") and
            not config.get("ExposedPorts") and not config.get("Healthcheck") and
            not config.get("OnBuild") and (observed or not config.get("Annotations")),
            "image hooks/volumes/ports/annotations forbidden")
    envs = ([IMAGE_PATH, "container=podman"],) if observed else ([IMAGE_PATH], [IMAGE_PATH, "HOME=/root"])
    require(config.get("Env") in envs, "unexpected container environment")
    host = data.get("HostConfig", {})
    if observed:
        validate_podman_fields(data, cid, name, token, evidence)
    else:
        require(not (set(host) - set(HOST_EXACT) - HOST_EMPTY - HOST_DEFAULT - {"IDMappings"}),
                "unrecognized HostConfig fields")
        for key, value in HOST_EXACT.items():
            require(key in host and exact_value(host[key], value), "unsafe or missing " + key)
        for key in HOST_EMPTY:
            require(host.get(key) in (None, [], {}), "unsafe " + key)
        for key in HOST_DEFAULT & set(host):
            require(host[key] in (None, False, 0, "", [], {}), "unsupported default " + key)
    safe_caps = {"CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_FOWNER", "CAP_FSETID",
                 "CAP_KILL", "CAP_NET_BIND_SERVICE", "CAP_SETFCAP", "CAP_SETGID",
                 "CAP_SETPCAP", "CAP_SETUID", "CAP_SYS_CHROOT", "CAP_AUDIT_WRITE"}
    for key in ("EffectiveCaps", "BoundingCaps"):
        require(isinstance(data.get(key), list) and set(data[key]) <= safe_caps,
                "unsafe or unknown capabilities")


def map_rows(text):
    require(isinstance(text, str) and 0 < len(text) <= 4096, "missing or oversized mapping")
    rows = []
    for line in text.splitlines():
        fields = line.split()
        require(len(fields) == 3 and all(re.fullmatch(r"0|[1-9][0-9]{0,9}", v) for v in fields),
                "unrecognized mapping")
        row = tuple(map(int, fields))
        require(row[2] > 0 and all(v + row[2] <= 2**32 - 1 for v in row[:2]),
                "invalid mapping range")
        rows.append(row)
    require(rows, "missing mapping")
    return rows


def validate_map(text, host_id):
    rows = map_rows(text)
    # Deliberately support one contiguous subordinate range, not keep-id/host.
    require(len(rows) == 1, "unsupported multi-range mapping")
    inside, outside, size = rows[0]
    require(inside == 0 and outside > 0 and size >= 65536 and
            outside + size <= 2**32 - 1 and not outside <= host_id < outside + size,
            "root must map exclusively to subordinate IDs")


def declared_maps(data):
    mappings = data.get("HostConfig", {}).get("IDMappings")
    require(isinstance(mappings, dict) and set(mappings) == {"UidMap", "GidMap"},
            "created-container UID/GID mapping evidence unavailable")
    result, formats = {}, set()
    for key in ("UidMap", "GidMap"):
        entries = mappings[key]
        require(isinstance(entries, list) and len(entries) == 1, "unrecognized created-container map")
        entry = entries[0]
        if isinstance(entry, str):
            require(len(entry) <= 32 and re.fullmatch(r"(0|[1-9][0-9]{0,9}):(0|[1-9][0-9]{0,9}):(0|[1-9][0-9]{0,9})", entry),
                    "unrecognized map string grammar")
            inside, start, count = map(int, entry.split(":"))
            formats.add("string")
        else:
            require(isinstance(entry, dict) and set(entry) == {"ContainerID", "HostID", "Size"}
                    and all(type(v) is int for v in entry.values()), "unrecognized map fields")
            inside, start, count = entry["ContainerID"], entry["HostID"], entry["Size"]
            formats.add("dict")
        require(inside == 0 and start > 0 and count == 65536 and start + count <= 2**32 - 1,
                "declared mapping must cover root in one bounded subordinate range")
        result[key] = (inside, start, count)
    require(len(formats) == 1, "inconsistent UID/GID declaration formats")
    return result


def parent_map_rows(text, host_id):
    rows = map_rows(text)
    require(rows[0] == (0, host_id, 1) and len(rows) >= 2, "rootless parent root mapping required")
    for inside, outside, size in rows:
        require(inside >= 0 and outside > 0 and size > 0 and
                inside + size <= 2**32 - 1 and outside + size <= 2**32 - 1,
                "invalid parent mapping range")
    for i, row in enumerate(rows):
        for other in rows[i + 1:]:
            for coordinate in (0, 1):
                require(row[coordinate] + row[2] <= other[coordinate] or
                        other[coordinate] + other[2] <= row[coordinate], "parent mapping alias/overlap")
    return rows


def validate_created_maps(data, parent_maps, uid, gid):
    require(isinstance(parent_maps, (list, tuple)) and len(parent_maps) == 2, "missing parent maps")
    declarations = declared_maps(data)
    translated_maps = {}
    for key, parent, host_id in (("UidMap", parent_maps[0], uid), ("GidMap", parent_maps[1], gid)):
        root, start, count = declarations[key]
        translated = []
        for inside, outside, size in parent_map_rows(parent, host_id):
            if inside <= start and start + count <= inside + size:
                translated.append(outside + start - inside)
        require(len(translated) == 1, "map cannot be resolved to host IDs")
        text = f"{root} {translated[0]} {count}"
        validate_map(text, host_id)
        translated_maps[key] = text
    return translated_maps


def validate_running_maps(data, parent_maps, probes, uid, gid):
    """Guest self maps use parent coordinates; init maps use host coordinates."""
    translated = validate_created_maps(data, parent_maps, uid, gid)
    declarations = declared_maps(data)
    for kind, key, host_id in (("uid_map", "UidMap", uid), ("gid_map", "GidMap", gid)):
        require(map_rows(probes.get("guest-" + kind)) == [declarations[key]],
                "guest mapping disagrees with parent-relative declaration")
        actual = probes.get("host-" + kind)
        validate_map(actual, host_id)
        require(map_rows(actual) == map_rows(translated[key]),
                "host init mapping disagrees with fresh parent composition")


def validate_namespaces(host, guest):
    require(set(host) == set(guest) == set(NAMESPACES), "missing namespace identities")
    for name in NAMESPACES:
        require(bool(re.fullmatch(name + r":\[\d+\]", guest[name])) and
                guest[name] != host[name], "shared or unknown " + name + " namespace")


def validate_mountinfo(text, cid):
    require(bool(text.strip()), "missing mountinfo")
    pseudo = {"proc", "sysfs", "tmpfs", "devpts", "mqueue", "cgroup2"}
    targets = {"/dev", "/dev/pts", "/dev/shm", "/dev/mqueue", "/proc", "/sys",
               "/sys/fs/cgroup", "/proc/acpi", "/proc/asound", "/proc/interrupts",
               "/proc/kcore", "/proc/keys", "/proc/latency_stats", "/proc/timer_list",
               "/proc/scsi", "/proc/sys", "/proc/sysrq-trigger", "/sys/firmware",
               "/sys/devices/virtual/powercap"}
    root_seen = False
    for line in text.splitlines():
        fields = line.split()
        require("-" in fields and len(fields) >= 10, "malformed mountinfo")
        sep = fields.index("-")
        require(sep >= 6 and len(fields) == sep + 4, "unrecognized mountinfo")
        root, target, fs, source = fields[3], fields[4], fields[sep + 1], fields[sep + 2]
        require(not any("sock" in x or "\\" in x for x in (root, target, source)),
                "socket or escaped mount reference")
        if target == "/":
            require(not root_seen and root == "/" and fs == "overlay" and source == "overlay",
                    "unrecognized root mount")
            root_seen = True
        elif target in {"/etc/hosts", "/etc/hostname", "/etc/resolv.conf"}:
            require(bool(re.fullmatch(r".*/" + cid + r"/userdata/(hosts|hostname|resolv.conf)", root))
                    and Path(root).name == Path(target).name, "unowned generated /etc mount")
        else:
            require(target in targets and fs in pseudo and
                    (root == "/" or root == "/null" or root.startswith("/sys") or
                     root.startswith("/bus") or root.startswith("/fs/cgroup")),
                    "host share or unknown mount: " + target)
    require(root_seen, "missing root mount")


class Approvals:
    """Zero indirect-operation approvals: unexpected review/removal is unsupported.

    Do not extract arbitrary approval text and replay it. All answers are literals.
    """
    def __init__(self, scenario):
        self.scenario = scenario
        self.pending = ""
        self.step = 0
        self.unsupported_reason = None

    def feed(self, chunk):
        self.pending += chunk.decode("utf-8", "strict").replace("\r\n", "\n")
        require(len(self.pending) <= 4 * 1024 * 1024, "transcript too large")
        if FINAL_PROMPT in self.pending:
            require(self.step == 0 and self.scenario in {"install", "wrong-approval", "eof"},
                    "unexpected final confirmation")
            before, self.pending = self.pending.split(FINAL_PROMPT, 1)
            self._reject_prompt(before)
            self._reject_prompt(self.pending)
            self.step = 1
            return {"install": b"apply\n", "wrong-approval": b"deny\n", "eof": b"\x04"}[self.scenario]
        if NATIVE_PROMPT in self.pending:
            require(self.step == 1 and self.scenario == "install", "unexpected native approval")
            before, self.pending = self.pending.split(NATIVE_PROMPT, 1)
            self._reject_prompt(before)
            self._reject_prompt(self.pending)
            self.step = 2
            return b"y\n"
        self._reject_prompt(self.pending)
        return b""

    @staticmethod
    def _reject_prompt(text):
        # Incomplete known prompts may arrive across reads; full unknown prompts
        # are rejected, and incomplete unknowns never receive input (timeout).
        require(not re.search(r'Type "(?!apply")[^"\n]+" to approve', text),
                "unexpected operation approval; no indirect approvals enumerated")
        require(not re.search(r"Do you want to continue\? \[[^\]]+\]", text),
                "unexpected native prompt")
        require(not re.search(r"(?i)(password|passphrase):\s*", text), "unexpected credential prompt")

    def finish(self):
        expected = {"install": 2, "wrong-approval": 1, "eof": 1}.get(self.scenario, 0)
        if self.step == 0 and expected > 0:
            result, executed = apply_records(self.pending.encode())
            reason = known_capability(self.pending)
            if result == "failure" and not executed and reason:
                self.unsupported_reason = reason
                return
        require(self.step == expected, "expected prompts not observed; failure/unclassified")


def pty_command(argv, approvals, timeout, stop, record, env=None):
    master, slave = pty.openpty()
    process = None
    transcript = bytearray()
    start = time.monotonic()
    try:
        process = subprocess.Popen(argv, stdin=slave, stdout=slave, stderr=slave,
                                   start_new_session=True, env=env)
        os.close(slave)
        slave = None
        while True:
            if time.monotonic() - start >= timeout:
                raise Refusal("PTY timeout; execution state uncertain")
            ready, _, _ = select.select([master], [], [], 0.05)
            if ready:
                try:
                    chunk = os.read(master, 65536)
                except OSError as exc:
                    if exc.errno != errno.EIO:
                        raise
                    chunk = b""
                if not chunk:
                    break
                transcript.extend(chunk)
                require(len(transcript) <= 4 * 1024 * 1024, "PTY output bound exceeded")
                answer = approvals.feed(chunk)
                if answer:
                    os.write(master, answer)
            elif process.poll() is not None:
                break
        rc = process.wait(timeout=max(0.1, timeout - (time.monotonic() - start)))
        approvals.finish()
        record({"argv": argv, "rc": rc, "transcript": transcript.decode("utf-8", "replace")})
        return rc, bytes(transcript)
    except BaseException:
        try:
            record({"argv": argv, "state": "uncertain", "transcript": transcript.decode("utf-8", "replace")})
        finally:
            try:
                stop()  # Even when evidence storage failed, stop the owned container.
            finally:
                if process and process.poll() is None:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
        raise
    finally:
        os.close(master)
        if slave is not None:
            os.close(slave)


def host_hashes():
    """Read regular package/metadata files only, never invoke host package tools."""
    result = {}
    # /etc/apt can contain authentication/proxy credentials: never read it.
    roots = ("/var/lib/dpkg", "/var/lib/apt", "/var/cache/apt")
    def traversal_error(error):
        raise error

    def signature(node):
        return (node.st_dev, node.st_ino, node.st_mode, node.st_size, node.st_mtime_ns,
                node.st_ctime_ns)

    for root in roots:
        require(Path(root).is_dir(), "host metadata directory unavailable: " + root)
        for directory, dirs, files in os.walk(root, followlinks=False, onerror=traversal_error):
            nodes = {}
            for name in dirs + files:
                path = Path(directory) / name
                node = os.lstat(path)  # Never suppress missing/inaccessible inventory entries.
                nodes[name] = node
                if stat.S_ISLNK(node.st_mode):
                    result[str(path)] = "symlink:" + os.readlink(path)
                    require(signature(node) == signature(os.lstat(path)), "symlink changed while hashing")
            for name in files:
                path, node = Path(directory) / name, nodes[name]
                if stat.S_ISLNK(node.st_mode):
                    continue
                fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
                try:
                    require(stat.S_ISREG(os.fstat(fd).st_mode), "nonregular host metadata file")
                    require(signature(node) == signature(os.fstat(fd)), "file changed before hashing")
                    with os.fdopen(fd, "rb", closefd=False) as stream:
                        result[str(path)] = hashlib.file_digest(stream, "sha256").hexdigest()
                    require(signature(node) == signature(os.fstat(fd)) == signature(os.lstat(path)),
                            "file changed while hashing")
                finally:
                    os.close(fd)
    return result


class Run:
    def __init__(self, evidence, timeout):
        self.evidence = evidence
        self.timeout = timeout
        self.token = uuid.uuid4().hex
        self.name = "cdots-apt-" + self.token
        self.cid = None
        self.verified = False
        self.probe_only = False
        self.uncertain = False
        self.stopped_verified = False
        self.evidence_errors = []
        self.sequence = 0
        self.results = {}
        self.hooks = evidence / "empty-hooks"
        self.hooks.mkdir(mode=0o700)
        self.base = ["/usr/bin/podman", "--remote=false", "--hooks-dir=" + str(self.hooks)]
        # Do not inherit command overrides or Go/network/build configuration.
        self.env = {k: os.environ[k] for k in ("HOME", "XDG_RUNTIME_DIR") if k in os.environ}
        self.env.update(PATH="/usr/bin:/bin", LANG="C", LC_ALL="C")

    def save(self, name, value):
        path = self.evidence / name
        with path.open("w", encoding="utf-8") as stream:
            json.dump(value, stream, indent=2, sort_keys=True)
            stream.write("\n")

    def record(self, value):
        self.sequence += 1
        self.save(f"command-{self.sequence:04d}.json", value)

    def storage_failure(self, error):
        self.uncertain = True
        if not hasattr(self, "evidence_errors"):
            self.evidence_errors = []
        self.evidence_errors.append(str(error))

    def safe_record(self, value):
        try:
            self.record(value)
        except (OSError, ValueError) as exc:
            self.storage_failure(exc)

    def capture(self, argv, env=None, cwd=None):
        return getattr(self, "executor", bounded_command)(
            argv, timeout=self.timeout, env=env or self.env, cwd=cwd,
            limit=getattr(self, "output_limit", OUTPUT_LIMIT))

    def command(self, argv, check=True, env=None, container_command=False, cwd=None):
        try:
            proc = self.capture(argv, env, cwd)
        except CaptureFailure as exc:
            self.uncertain = True
            try:
                self.safe_record({"argv": argv, "rc": exc.rc, "state": "uncertain-" + exc.state,
                                  "stdout": exc.stdout.decode("utf-8", "replace"),
                                  "stderr": exc.stderr.decode("utf-8", "replace")})
            finally:
                if container_command and self.cid:
                    self.stop()
            raise
        try:
            self.record({"argv": argv, "cwd": str(cwd) if cwd else None, "rc": proc.returncode,
                         "stdout": proc.stdout.decode("utf-8", "replace"),
                         "stderr": proc.stderr.decode("utf-8", "replace")})
        except (OSError, ValueError) as exc:
            self.storage_failure(exc)
            if container_command and self.cid:
                self.stop()
            raise
        require(not check or proc.returncode == 0, "command failed: " + str(argv))
        return proc.returncode, proc.stdout

    def safety_command(self, *args, check=True):
        """Critical inspect/stop/state/removal never depend on evidence storage."""
        argv = self.base + list(args)
        try:
            proc = self.capture(argv)
        except CaptureFailure as exc:
            self.uncertain = True
            self.safe_record({"argv": argv, "rc": exc.rc, "state": exc.state,
                              "stdout": exc.stdout.decode("utf-8", "replace"),
                              "stderr": exc.stderr.decode("utf-8", "replace")})
            raise
        self.safe_record({"argv": argv, "rc": proc.returncode,
                          "stdout": proc.stdout.decode("utf-8", "replace"),
                          "stderr": proc.stderr.decode("utf-8", "replace")})
        require(not check or proc.returncode == 0, "safety command failed: " + str(argv))
        return proc.returncode, proc.stdout

    def podman(self, *args, check=True, container_command=False):
        return self.command(self.base + list(args), check=check,
                            container_command=container_command)

    def inspect(self, critical=False):
        command = self.safety_command if critical else self.podman
        _, out = command("container", "inspect", self.cid)
        parsed = json.loads(out)
        require(isinstance(parsed, list) and len(parsed) == 1, "ambiguous container inspect")
        return parsed[0]

    def owned(self, critical=False):
        require(self.cid is not None and bool(FULL_ID.fullmatch(self.cid)), "no full owned container ID")
        data = self.inspect(critical=critical)
        validate_identity(data, self.cid, self.name, self.token)
        return data

    def stop_owned(self):
        self.stopped_verified = False
        self.owned(critical=True)
        rc, _ = self.safety_command("stop", "--time=2", self.cid, check=False)
        data = self.owned(critical=True)
        require(data.get("State", {}).get("Running") is False, "cannot confirm container stopped")
        self.stopped_verified = True
        require(rc == 0, "stop returned failure despite stopped observation")

    def stop(self):
        self.uncertain = True
        self.stop_owned()

    def exec(self, *args, check=True):
        require(self.verified and not getattr(self, "probe_only", False),
                "package/fixture command before completed apply isolation probes")
        self.owned()
        return self.podman("exec", self.cid, *args, check=check, container_command=True)

    def probe(self, *args):
        # This path is exclusively called by verify_probes with fixed read-only
        # literals. It is not a package-command escape hatch.
        if getattr(self, "probe_only", False):
            allowed = {( '/usr/bin/readlink', '/proc/self/ns/' + n) for n in NAMESPACES}
            allowed |= {( '/bin/cat', p) for p in ('/proc/self/uid_map', '/proc/self/gid_map',
                         '/proc/self/status', '/proc/self/mountinfo', '/etc/os-release')}
            allowed |= {('/usr/bin/id', '-u'), ('/usr/bin/id', '-g')}
            require(args in allowed, "non-probe command forbidden in probe-only mode")
        return self.podman("exec", self.cid, *args, container_command=True)[1].decode().strip()

    def create(self, probe_only=False):
        self.probe_only = probe_only
        _, out = self.podman("info", "--format=json")
        validate_runtime(json.loads(out))
        _, out = self.podman("image", "inspect", IMAGE)
        images = json.loads(out)
        require(len(images) == 1, "ambiguous image")
        validate_image(images[0])
        parent_maps = [self.podman("unshare", "/bin/cat", "/proc/self/" + kind)[1].decode()
                       for kind in ("uid_map", "gid_map")]
        # Only the unique cidfile recovers ownership if create times out.
        cidfile = self.evidence / "container.cid"
        self.expected_create = create_command(self.evidence, self.name, self.token)
        try:
            _, out = self.command(self.expected_create)
            self.cid = out.decode().strip()
        finally:
            if self.cid is None and cidfile.is_file():
                candidate = cidfile.read_text().strip()
                if FULL_ID.fullmatch(candidate):
                    self.cid = candidate
        require(self.cid is not None, "container creation produced no full ID; uncertain ownership")
        data = self.owned()
        self.save("created-inspect.json", data)
        self.save("probes.json", {"stage": "prestart", "started": False,
                                  "parent_maps": parent_maps})
        validate_container(data, self.cid, self.name, self.token, self.evidence)
        require(data.get("Config", {}).get("CreateCommand") == self.expected_create,
                "created declaration differs from exact issued argv")
        validate_created_maps(data, parent_maps, os.getuid(), os.getgid())
        self.podman("start", self.cid, container_command=True)
        self.verify_probes()

    def verify_probes(self):
        self.verified = False
        evidence = {"stage": "poststart", "started": True, "collected": False, "complete": False}

        def collect(key, *args):
            value = self.probe(*args)
            evidence[key] = value
            return value

        try:
            data = self.owned()
            self.save("running-inspect.json", data)
            pid = data.get("State", {}).get("Pid")
            require(type(pid) is int and pid > 0, "missing running init PID")
            host_ns = {n: os.readlink("/proc/self/ns/" + n) for n in NAMESPACES}
            evidence["host_namespaces"] = host_ns
            guest_ns = {}
            evidence["guest_namespaces"] = guest_ns
            for n in NAMESPACES:
                guest_ns[n] = collect("namespace-" + n, "/usr/bin/readlink", "/proc/self/ns/" + n)
            # Collect the complete fixed read-only set before validators. This
            # preserves diagnostic evidence even when namespace-relative maps
            # differ; collection is not execution authority.
            for kind in ("uid_map", "gid_map"):
                evidence["host-" + kind] = Path(f"/proc/{pid}/{kind}").read_text()
                collect("guest-" + kind, "/bin/cat", "/proc/self/" + kind)
            collect("uid", "/usr/bin/id", "-u")
            collect("gid", "/usr/bin/id", "-g")
            status = collect("status", "/bin/cat", "/proc/self/status")
            mountinfo = collect("mountinfo", "/bin/cat", "/proc/self/mountinfo")
            release = collect("os_release", "/bin/cat", "/etc/os-release")
            # Re-read the runtime parent frame; never authorize from cached
            # prestart coordinates or compare maps expressed in different frames.
            evidence["parent_maps"] = [
                self.podman("unshare", "/bin/cat", "/proc/self/" + kind)[1].decode()
                for kind in ("uid_map", "gid_map")]
            evidence["collected"] = True
            self.save("probes.json", evidence)
            validate_container(data, self.cid, self.name, self.token, self.evidence)
            require(data.get("Config", {}).get("CreateCommand") == self.expected_create,
                    "running declaration differs from exact issued argv")
            validate_namespaces(host_ns, guest_ns)
            validate_running_maps(data, evidence["parent_maps"], evidence, os.getuid(), os.getgid())
            require(evidence["uid"] == evidence["gid"] == "0", "fixture must be userns root")
            require(re.search(r"^NoNewPrivs:\s+1$", status, re.M) is not None, "no_new_privs missing")
            require(re.search(r"^Seccomp:\s+2$", status, re.M) is not None, "seccomp filtering missing")
            validate_mountinfo(mountinfo, self.cid)
            require(all(re.search(r"^" + line + r"$", release, re.M) for line in
                        ('ID=debian', 'VERSION_ID="12"', 'VERSION_CODENAME=bookworm')),
                    "Debian 12/bookworm required")
            if not self.probe_only:
                apt_version = collect("apt_version", "/usr/bin/apt-get", "--version")
                require(apt_version.splitlines()[0] == "apt 2.6.1 (amd64)", "APT 2.6.1 required")
                self.verified = True
            evidence["complete"] = True
        except BaseException as exc:
            evidence["error"] = str(exc)
            raise
        finally:
            self.save("probes.json", evidence)

    def copy(self, source, destination):
        require(self.verified and not getattr(self, "probe_only", False), "copy before apply isolation verification")
        self.owned()
        self.podman("cp", str(source), self.cid + ":" + destination, container_command=True)
        _, output = self.exec("/usr/bin/sha256sum", destination)
        require(output.decode().split()[0] == hashlib.sha256(source.read_bytes()).hexdigest(),
                "copied file checksum mismatch")

    def package_state(self, name):
        rc, out = self.exec("/usr/bin/dpkg-query", "-W",
                            "-f=${binary:Package}\t${Version}\t${Status}\n", check=False)
        require(rc == 0, "complete package manifest query failed")
        self.save(name + "-packages.json", {"rc": rc, "output": out.decode()})
        return out

    def scenario(self, scenario):
        before = self.package_state(scenario + "-before")
        installed = b"git\t" in before and any(line.startswith(b"git\t") and
                    line.endswith(b"install ok installed") for line in before.splitlines())
        if scenario in {"install", "wrong-approval", "eof", "non-tty", "native-config", "apt-error"} and installed:
            self.results[scenario] = {"status": "unsupported", "reason": "git already installed; never remove to force fixture"}
            return
        if scenario == "no-op" and not installed:
            self.results[scenario] = {"status": "unsupported", "reason": "git absent; select install,no-op to share successful fixture"}
            return
        if scenario == "protected":
            self.results[scenario] = {"status": "unsupported", "reason": "catalog has no removal fixture; no protection bypass"}
            return
        if scenario == "apt-error":
            malformed = self.evidence / "malformed.list"
            malformed.write_text("this-is-not-a-valid-deb-line\n")
            self.copy(malformed, "/etc/apt/sources.list.d/phase5a-malformed.list")
        args = ["exec"]
        if scenario == "native-config":
            # A benign debug option is non-default. It does not disable a native
            # safeguard, and metadata trust rejects it before asking approval.
            alternate = self.evidence / "nondefault-dpkg.cfg"
            alternate.write_text("debug 0\n")
            self.copy(alternate, "/etc/dpkg/dpkg.cfg.d/phase5a")
        argv = self.base + args + (["-it"] if scenario != "non-tty" else []) + [self.cid, "/tmp/cdots", "apply", "git"]
        self.owned()
        if scenario == "non-tty":
            rc, transcript = self.command(argv, check=False, container_command=True)
        else:
            def record_transcript(value):
                (self.evidence / (scenario + ".pty.txt")).write_text(value["transcript"])
                self.record(value)
            approvals = Approvals(scenario)
            rc, transcript = pty_command(argv, approvals, self.timeout,
                                         self.stop, record_transcript, env=self.env)
        (self.evidence / (scenario + ".pty.txt")).write_bytes(transcript)
        after = self.package_state(scenario + "-after")
        if scenario != "non-tty" and approvals.unsupported_reason:
            require(rc != 0 and before == after, "blocked fixture changed package state")
            self.results[scenario] = {"status": "unsupported", "rc": rc,
                                      "reason": approvals.unsupported_reason}
            return
        result, executed = apply_records(transcript)
        expected = {"install": (0, "success", True), "no-op": (0, "no-action", False),
                    "wrong-approval": (130, "cancellation", False), "eof": (130, "cancellation", False)}
        if scenario in expected:
            require((rc, result, executed) == expected[scenario],
                    "scenario outcome did not match expected " + scenario)
        else:
            require(rc != 0 and result == "failure" and not executed,
                    "rejection fixture unexpectedly executed")
            if scenario == "non-tty":
                require(b"genuine input and output terminals" in transcript, "missing non-TTY rejection")
            if scenario == "native-config":
                require(b"native dpkg configuration" in transcript,
                        "missing native configuration rejection")
            if scenario == "apt-error":
                require(b"Malformed" in transcript or b"malformed" in transcript,
                        "missing controlled APT error diagnostic")
        if scenario != "install":
            require(before == after, "denied/no-op fixture changed package post-state")
        else:
            require(any(line.startswith(b"git\t") and line.endswith(b"install ok installed")
                        for line in after.splitlines()), "git not independently confirmed installed")
        self.results[scenario] = {"status": "passed", "rc": rc}

    def cleanup(self):
        if self.cid is None:
            return
        data = self.owned(critical=True)  # Refuse untrusted identity before all actions.
        try:
            try:
                self.save("final-inspect.json", data)
            except (OSError, ValueError) as exc:
                self.storage_failure(exc)
            self.safety_command("logs", self.cid)  # Bounded retention before removal.
        finally:
            self.stop_owned()  # Logs/final-inspect failure cannot suppress a safe stop.
        require(not getattr(self, "evidence_errors", []), "evidence incomplete; stopped object retained")
        self.safety_command("rm", self.cid)  # Only the verified stopped full ID; no force/volumes.
        require(not getattr(self, "evidence_errors", []), "cleanup recording failed; state uncertain")


def private_evidence():
    # Keep proof in this worktree rather than volatile /tmp. Never follow an
    # evidence-root symlink or change permissions on an existing user directory.
    root = Path(__file__).resolve().parent / "evidence"
    require(not root.is_symlink(), "evidence root must not be a symlink")
    root.mkdir(mode=0o700, exist_ok=True)
    metadata = root.stat()
    require(stat.S_ISDIR(metadata.st_mode) and metadata.st_uid == os.getuid()
            and stat.S_IMODE(metadata.st_mode) == 0o700,
            "evidence root must be an owned private directory")
    evidence = Path(tempfile.mkdtemp(prefix="cdots-phase5a-", dir=root))
    os.chmod(evidence, 0o700)
    print("Evidence: " + str(evidence), flush=True)
    return evidence


def run_probe_only(args):
    """Inert container lifecycle + isolation/OS probes only; no apply call path."""
    validate_host(os.getuid(), os.environ)
    validate_host(os.geteuid(), os.environ)
    require(os.path.isfile("/usr/bin/podman"), "local /usr/bin/podman missing")
    run = Run(private_evidence(), args.timeout)
    summary = {"mode": "probe-only", "status": "failure", "image": IMAGE,
               "digest": DIGEST, "cleanup_errors": []}
    try:
        run.create(probe_only=True)
        summary["status"] = "passed"
    except (Refusal, OSError, ValueError, subprocess.SubprocessError, KeyboardInterrupt) as exc:
        summary["error"] = str(exc)
    finally:
        try:
            run.cleanup()
        except (Refusal, OSError, ValueError, subprocess.SubprocessError) as exc:
            summary["cleanup_errors"].append(str(exc))
        summary["container"] = run.cid
        summary["evidence_errors"] = run.evidence_errors
        summary["stopped_verified"] = getattr(run, "stopped_verified", False)
        if run.uncertain or summary["cleanup_errors"]:
            summary["status"] = "uncertain"
        run.save("summary.json", summary)
    return 0 if summary["status"] == "passed" else 1


def run_disposable(args):
    validate_host(os.getuid(), os.environ)
    validate_host(os.geteuid(), os.environ)
    require(os.path.isfile("/usr/bin/podman"), "local /usr/bin/podman missing")
    evidence = private_evidence()
    run = Run(evidence, args.timeout)
    before = None
    summary = {"status": "failure", "live_execution_authorization": "explicit disposable opt-in",
               "image": IMAGE, "digest": DIGEST, "scenarios": {}, "cleanup_errors": [],
               "host_unchanged": False}
    runs = []
    try:
        before = host_hashes()
        run.save("host-before.json", before)
        repo = Path(__file__).resolve().parents[3]
        binary = evidence / "cdots"
        build_env = {"PATH": "/usr/bin:/bin", "HOME": str(evidence), "CGO_ENABLED": "0",
                     "GOOS": "linux", "GOARCH": "amd64", "GOPROXY": "off", "GOSUMDB": "off",
                     "GOTOOLCHAIN": "local", "GOENV": "off", "GOCACHE": str(evidence / "go-cache")}
        # Explicit path, no shell or arbitrary binary/apply fallback.
        compilers = [p for p in ("/usr/local/go/bin/go", "/usr/bin/go") if Path(p).is_file()]
        require(bool(compilers), "no compiler at an enumerated local Go path")
        run.command([compilers[0], "build", "-o", str(binary), "./cmd/cdots"],
                    env=build_env, cwd=repo)
        for scenario in args.scenario:
            if scenario == "protected":
                summary["scenarios"][scenario] = {"status": "unsupported",
                    "reason": "catalog has no removal fixture; no protection bypass"}
                continue
            # Only install/no-op share a disposable container; all rejection and
            # error fixtures get a fresh independently verified container.
            if scenario == "no-op" and runs and runs[-1].results.get("install", {}).get("status") == "passed":
                current = runs[-1]
            else:
                unit = evidence / (scenario + "-" + uuid.uuid4().hex)
                unit.mkdir(mode=0o700)
                current = Run(unit, args.timeout)
                runs.append(current)
                current.create()
                current.copy(binary, "/tmp/cdots")
                current.exec("/usr/bin/apt-get", "update")
            current.scenario(scenario)
            summary["scenarios"][scenario] = current.results[scenario]
        summary["status"] = "passed" if all(x["status"] == "passed" for x in summary["scenarios"].values()) else "unsupported"
    except (Refusal, OSError, ValueError, subprocess.SubprocessError, KeyboardInterrupt) as exc:
        summary["error"] = str(exc)
    finally:
        for current in reversed(runs):
            try:
                current.cleanup()
            except (Refusal, OSError, ValueError, subprocess.SubprocessError) as exc:
                summary["cleanup_errors"].append({"container": current.cid, "error": str(exc), "state": "uncertain"})
            if current.uncertain:
                summary["status"] = "uncertain"
            if current.evidence_errors:
                summary["cleanup_errors"].append({"container": current.cid,
                    "evidence_errors": current.evidence_errors, "state": "uncertain"})
        try:
            after = host_hashes()
            run.save("host-after.json", after)
            summary["host_unchanged"] = before is not None and before == after
            require(before is not None and before == after,
                    "host baseline missing or package/apt metadata changed; attribution unknown")
        except (Refusal, OSError) as exc:
            summary["host_unchanged"] = False
            summary["host_hash_error"] = str(exc)
            summary["status"] = "failure"
        if summary["cleanup_errors"]:
            summary["status"] = "uncertain"
        run.save("summary.json", summary)
    return 0 if summary["status"] == "passed" else 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--run-disposable", action="store_true", help="explicit apply-fixture opt-in; safety review required")
    mode.add_argument("--probe-only", action="store_true", help="inert disposable isolation/OS probes; no package tools/build/copy/apply")
    parser.add_argument("--scenario", action="append", choices=SCENARIOS)
    parser.add_argument("--timeout", type=float, default=180)
    args = parser.parse_args(argv)
    if not (args.run_disposable or args.probe_only):
        print("Refused: choose an explicit disposable mode (review required).", file=sys.stderr)
        return 2
    if (not 1 <= args.timeout <= 600 or (args.probe_only and args.scenario) or
            (args.run_disposable and (not args.scenario or len(args.scenario) != len(set(args.scenario))))):
        print("Timeout must be 1..600; only apply mode takes unique scenarios.", file=sys.stderr)
        return 2
    try:
        return run_probe_only(args) if args.probe_only else run_disposable(args)
    except (Refusal, OSError, ValueError) as exc:
        print("Refused: " + str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
