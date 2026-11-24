"""Fixed ordinary proc observations; validation and execution grants live elsewhere."""
import os
import re
import stat

PROC_ROOT = "/proc"  # Test patching only; no CLI/environment override.
FIELD_LIMIT = 131072
FIELDS = ("stat", "status", "uid_map", "gid_map", "mountinfo")
NAMESPACES = ("user", "mnt", "pid", "net", "ipc", "uts", "cgroup")


def marker(frame, field):
    return "@@CDOTS:" + frame + ":" + field + "@@\n"


# No interpolated input: two fixed proc frames in one owned exec, no package IO.
_script = ["set -eu", "self=$$"]
for _frame, _pid in (("init", "1"), ("collector", "$self")):
    for _field in FIELDS:
        _script += ["printf '%s\\n' '" + marker(_frame, _field).rstrip("\n") + "'",
                    '/bin/cat "/proc/' + _pid + '/' + _field + '"']
    for _name in NAMESPACES:
        _script += ["printf '%s\\n' '" + marker(_frame, "ns-" + _name).rstrip("\n") + "'",
                    '/usr/bin/readlink "/proc/' + _pid + '/ns/' + _name + '"']
GUEST_SCRIPT = "\n".join(_script)


def parse_guest(text):
    """Reject missing, duplicate, reordered, oversized or ambiguous frames."""
    if not isinstance(text, str) or len(text) > 4 * 1024 * 1024:
        raise ValueError("invalid guest evidence size/type")
    expected = [(frame, field) for frame in ("init", "collector")
                for field in FIELDS + tuple("ns-" + n for n in NAMESPACES)]
    headers = [marker(*key) for key in expected]
    matches = list(re.finditer(r"^@@CDOTS:[^\n]+@@\n", text, re.M))
    if ([m.group() for m in matches] != headers or not matches or matches[0].start() != 0):
        raise ValueError("missing/duplicate/reordered guest evidence markers")
    result = {frame: {"namespaces": {}} for frame in ("init", "collector")}
    for i, (frame, field) in enumerate(expected):
        end = matches[i + 1].start() if i + 1 < len(matches) else len(text)
        value = text[matches[i].end():end]
        if not value or len(value) > FIELD_LIMIT:
            raise ValueError("missing or oversized guest evidence field")
        if field.startswith("ns-"):
            name = field[3:]
            if not re.fullmatch(re.escape(name) + r":\[[0-9]+\]\n", value):
                raise ValueError("ambiguous guest namespace")
            result[frame]["namespaces"][name] = value.rstrip("\n")
        else:
            result[frame][field] = value
    return result


def _read(fd, name):
    node = os.open(name, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
    try:
        if not stat.S_ISREG(os.fstat(node).st_mode):
            raise ValueError("nonregular proc evidence field")
        data = bytearray()
        while chunk := os.read(node, min(65536, FIELD_LIMIT + 1 - len(data))):
            data.extend(chunk)
            if len(data) > FIELD_LIMIT:
                raise ValueError("oversized proc evidence field")
        return bytes(data).decode("utf-8", "strict")
    finally:
        os.close(node)


def _frame(fd):
    result = {name: _read(fd, name) for name in FIELDS}
    ns = os.open("ns", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
    try:
        result["namespaces"] = {name: os.readlink(name, dir_fd=ns) for name in NAMESPACES}
    finally:
        os.close(ns)
    return result


def collect_host(pid):
    """Caller must first bind pid to its exact owned running container.

    Process directories are held open, not looked up again by recycled PID.
    Any permission, liveness, decoding or size error propagates; no fallback.
    """
    if type(pid) is not int or not 0 < pid < 2**31:
        raise ValueError("invalid owned init PID")
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    root = os.open(PROC_ROOT, flags)
    try:
        controller = os.open(str(os.getpid()), flags, dir_fd=root)
        try:
            own = _frame(controller)
        finally:
            os.close(controller)
        init = os.open(str(pid), flags, dir_fd=root)
        try:
            return {"controller": own, "init": _frame(init), "init_stat_after": _read(init, "stat")}
        finally:
            os.close(init)
    finally:
        os.close(root)
