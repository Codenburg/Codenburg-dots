"""Bounded, lexical checks for the retained Podman 4.9.3 mount profile.

Full checks require fresh, caller-validated local-rootless info and inspect.
This module never reads physical paths or resolves storage symlinks. In
particular, overlay link aliases are opaque diagnostics under D1's explicit
trusted-runtime boundary, never normalized or treated as physical proof.
Passing these checks is not a claim of physical storage isolation or a substitute
for the caller's namespace, UID, security, and host-integrity gates.
"""
import re


class MountValidationError(ValueError):
    """A precise blocker to accepting the observed mount evidence."""


def _require(condition, message):
    if not condition:
        raise MountValidationError(message)


def _path(value):
    return (isinstance(value, str) and value.startswith("/") and
            not any(c in value for c in "\\\x00\n\r\t ,:") and
            all(part not in {"", ".", ".."} for part in value.split("/")[1:]))


def _options(text):
    parts = text.split(",")
    keys = [part.split("=", 1)[0] for part in parts]
    _require(all(parts) and len(set(keys)) == len(keys), "empty or duplicate mount option")
    _require(not ({"ro", "rw"} <= set(keys)), "contradictory mount access flags")
    return dict(part.split("=", 1) if "=" in part else (part, None) for part in parts)


def _profile():
    # target -> (filesystem root, fstype, source, required effective flags)
    safe = {"nosuid", "nodev", "noexec"}
    rows = {
        "/proc": ("/", "proc", "proc", {"rw"} | safe),
        "/sys": ("/", "sysfs", "sysfs", {"ro"} | safe),
        "/sys/fs/cgroup": ("/", "cgroup2", "cgroup2", {"ro"} | safe),
        "/dev": ("/", "tmpfs", "tmpfs", {"rw", "nosuid"}),
        "/dev/pts": ("/", "devpts", "devpts", {"rw", "nosuid", "noexec"}),
        "/dev/shm": ("/", "tmpfs", "shm", {"rw"} | safe),
        "/dev/mqueue": ("/", "mqueue", "mqueue", {"rw"} | safe),
    }
    for name in ("null", "zero", "full", "tty", "random", "urandom"):
        rows["/dev/" + name] = ("/" + name, "devtmpfs", "udev", {"rw", "nosuid", "noexec"})
    for name in ("kcore", "keys", "latency_stats", "timer_list"):
        rows["/proc/" + name] = ("/null", "devtmpfs", "udev", {"ro", "nosuid"})
    for name in ("asound", "bus", "fs", "irq", "sys", "sysrq-trigger"):
        rows["/proc/" + name] = ("/" + name, "proc", "proc", {"ro"} | safe)
    for target in ("/proc/acpi", "/proc/scsi", "/sys/firmware", "/sys/dev/block",
                   "/sys/devices/virtual/powercap"):
        rows[target] = ("/", "tmpfs", "tmpfs", {"ro"})
    return rows


_GENERATED = {"/etc/hosts": "hosts", "/etc/hostname": "hostname",
              "/etc/resolv.conf": "resolv.conf", "/run/.containerenv": ".containerenv"}
_PROFILE = _profile()


def _lower_layers(value, graph_root, upper, label):
    # Recognize only the reviewed backend spellings; never normalize ../diff1.
    prefix = re.escape(graph_root) + r"/overlay/"
    pattern = prefix + r"(?:[0-9a-f]{64}/diff|l/[A-Za-z0-9]+(?:/\.\./diff1)?)"
    _require(isinstance(value, str) and 0 < len(value) <= 16384 and
             all(re.fullmatch(pattern, p) for p in value.split(":")) and
             len(set(value.split(":"))) == len(value.split(":")),
             "unrecognized " + label + " overlay lower layers")
    _require(upper not in value.split(":"), "overlay lower layer aliases writable upper")


def _controller_files(text, paths):
    """Translate supported runtime paths using controller filesystem coordinates.

    No filesystem IO: a unique deepest component-covering mount supplies its
    root, device and filesystem identity. Stacked/duplicate targets are unknown.
    """
    _require(isinstance(text, str) and bool(text.strip()) and len(text) <= 131072,
             "missing or oversized controller mountinfo")
    lines = text.splitlines()
    _require(len(lines) <= 4096, "too many controller mountinfo records")
    mounts, ids = {}, set()
    for line in lines:
        fields = line.split()
        _require(fields.count("-") == 1 and len(fields) >= 10,
                 "malformed controller mountinfo")
        sep = fields.index("-")
        _require(sep >= 6 and len(fields) == sep + 4, "malformed controller mountinfo")
        mid, parent, device, root, target, flags = fields[:6]
        fs, source, superflags = fields[sep + 1:]
        _require(re.fullmatch(r"[1-9][0-9]*", mid) and
                 re.fullmatch(r"[1-9][0-9]*", parent) and
                 re.fullmatch(r"[0-9]+:[0-9]+", device) and
                 all(p == "/" or _path(p) for p in (root, target)) and
                 all(re.fullmatch(r"(?:shared|master|propagate_from):[1-9][0-9]*|unbindable", p)
                     for p in fields[6:sep]) and
                 not any("\\" in p or "\x00" in p for p in fields),
                 "invalid controller mountinfo coordinates")
        _require(mid not in ids and target not in mounts,
                 "ambiguous controller mountinfo covering mounts")
        _options(flags)
        _options(superflags)
        ids.add(mid)
        mounts[target] = (root, device, fs, source)
    _require("/" in mounts, "missing controller mountinfo root")
    result = {}
    for destination, path in paths.items():
        covering = [p for p in mounts if p == "/" or path == p or path.startswith(p + "/")]
        target = max(covering, key=len)
        root, device, fs, source = mounts[target]
        _require(path != target, "controller mountinfo runtime file is a mount directory")
        relative = path[len(target):] if target != "/" else path
        translated = (root.rstrip("/") + relative)
        result[destination] = (translated, device, fs, source)
    return result


def _runtime(data, cid, graph_root, run_root, uid):
    _require(type(uid) is int and 0 < uid < 2**32, "missing actual rootless UID")
    _require(_path(graph_root) and graph_root not in {
        "/home", "/root", "/var", "/var/lib", "/var/lib/dpkg", "/var/lib/apt",
        "/var/cache/apt", "/run", "/tmp", "/usr", "/etc"}, "missing or broad graphRoot")
    _require(run_root == f"/run/user/{uid}/containers", "unrecognized UID-bound runRoot")
    _require(isinstance(data, dict) and data.get("Id") == cid and
             data.get("Driver") == "overlay" and data.get("Mounts") == [] and
             isinstance(data.get("State"), dict) and data["State"].get("Running") is True,
             "missing matching running inspect")
    driver = data.get("GraphDriver")
    _require(isinstance(driver, dict) and driver.get("Name") == "overlay" and
             isinstance(driver.get("Data"), dict), "missing overlay inspect data")
    overlay = driver["Data"]
    upper = overlay.get("UpperDir")
    _require(isinstance(upper, str) and bool(re.fullmatch(
        re.escape(graph_root) + r"/overlay/[0-9a-f]{64}/diff", upper)),
        "overlay upperdir is outside inspected graphRoot layer")
    layer = upper[:-len("diff")]
    _require(overlay.get("WorkDir") == layer + "work" and
             overlay.get("MergedDir") == layer + "merged", "overlay layer paths disagree")
    lower = overlay.get("LowerDir")
    _lower_layers(lower, graph_root, upper, "inspected")
    userdata = run_root + "/overlay-containers/" + cid + "/userdata/"
    for field, name in (("HostsPath", "hosts"), ("HostnamePath", "hostname"),
                        ("ResolvConfPath", "resolv.conf")):
        _require(data.get(field) == userdata + name, "generated inspect path disagrees: " + field)
    return overlay


def validate_mountinfo(text, cid, *, require_full=False, running_inspect=None,
                       graph_root=None, run_root=None, uid=None, controller_mountinfo=None):
    """Return None for accepted evidence; otherwise raise MountValidationError.

    Minimal mode retains root-only fixture support, not complete isolation.
    Full mode requires controller_mountinfo from the controller's own namespace,
    every retained boundary and exact runtime file root/device bindings. The caller
    must validate fresh local-rootless runtime/image/CID evidence under D1's trust
    boundary and use full mode for live checks. Supported lowerdir aliases/counts
    are backend diagnostics, NOT physical provenance. No path is resolved here.
    """
    _require(isinstance(cid, str) and bool(re.fullmatch(r"[0-9a-f]{64}", cid)), "invalid container ID")
    _require(isinstance(text, str) and bool(text.strip()) and len(text) <= 131072,
             "missing or oversized mountinfo")
    lines = text.splitlines()
    _require(len(lines) <= 128, "too many mountinfo records")
    overlay = _runtime(running_inspect, cid, graph_root, run_root, uid) if require_full else None
    generated = None
    if require_full:
        userdata = run_root + "/overlay-containers/" + cid + "/userdata/"
        paths = {target: userdata + name for target, name in _GENERATED.items()}
        generated = _controller_files(controller_mountinfo, paths)
    seen, ids, parents = {}, {}, {}
    root_options = None
    for line in lines:
        fields = line.split()
        _require(fields.count("-") == 1 and len(fields) >= 10, "malformed mountinfo")
        sep = fields.index("-")
        _require(sep == 6 and len(fields) == 10, "unrecognized mountinfo optional fields")
        mount_id, parent, device, root, target, flags, _, fs, source, superflags = fields
        _require(bool(re.fullmatch(r"[1-9][0-9]*", mount_id)) and
                 bool(re.fullmatch(r"[1-9][0-9]*", parent)) and
                 bool(re.fullmatch(r"[0-9]+:[0-9]+", device)), "invalid mount identity")
        _require(not any("sock" in x or "\\" in x or "\x00" in x
                         for x in (root, target, source, flags, superflags)),
                 "socket or escaped mount reference")
        _require(target not in seen and mount_id not in ids, "duplicate mount target or ID: " + target)
        options, super_options = _options(flags), _options(superflags)
        _require(set(options) <= {"ro", "rw", "nosuid", "nodev", "noexec", "relatime"} and
                 all(v is None for v in options.values()) and
                 bool({"ro", "rw"} & set(options)), "unrecognized effective mount flags: " + target)
        if target == "/":
            _require((root, fs, source) == ("/", "overlay", "overlay") and "rw" in options,
                     "unrecognized root mount")
            root_options = super_options
        elif target in _GENERATED:
            expected = "/containers/overlay-containers/" + cid + "/userdata/" + _GENERATED[target]
            if require_full:
                _require((root, device, fs, source) == generated[target] and
                         (fs, source) == ("tmpfs", "tmpfs"),
                         "unowned generated mount: " + target)
            else:
                _require((root, fs, source) == (expected, "tmpfs", "tmpfs"),
                         "unowned generated mount: " + target)
            _require({"rw", "nosuid", "nodev"} <= set(options), "unsafe generated mount flags: " + target)
            if require_full:
                _require(super_options.get("uid") == str(uid) and
                         super_options.get("gid") == str(uid) and super_options.get("mode") == "700",
                         "generated mount superblock is not UID-bound: " + target)
        else:
            _require(target in _PROFILE, "host share or unknown mount: " + target)
            expected_root, expected_fs, expected_source, required = _PROFILE[target]
            _require((root, fs, source) == (expected_root, expected_fs, expected_source),
                     "unrecognized mount tuple: " + target)
            _require(required <= set(options), "missing effective mount protection: " + target)
        if target != "/":
            allowed = {"ro", "rw", "size", "nr_inodes", "mode", "uid", "gid", "inode64",
                       "ptmxmode", "nsdelegate", "memory_recursiveprot"}
            _require(set(super_options) <= allowed and
                     all(v is None if k in {"ro", "rw", "inode64", "nsdelegate", "memory_recursiveprot"}
                         else isinstance(v, str) and bool(re.fullmatch(r"[0-9]+k?", v))
                         for k, v in super_options.items()), "unrecognized superblock options: " + target)
        seen[target] = mount_id
        ids[mount_id] = target
        parents[target] = parent
    _require("/" in seen, "missing root mount")
    if not require_full:
        return None
    missing = (set(_PROFILE) | set(_GENERATED)) - set(seen)
    _require(not missing, "missing required mount boundaries: " + ", ".join(sorted(missing)))
    for target, parent in parents.items():
        if target == "/":
            _require(parent not in ids, "root mount parent is inside container profile")
            continue
        parent_target = ("/dev" if target.startswith("/dev/") else
                         "/proc" if target.startswith("/proc/") else
                         "/sys" if target.startswith("/sys/") else "/")
        _require(ids.get(parent) == parent_target, "unrecognized mount parent: " + target)
    _require(set(root_options) == {"rw", "lowerdir", "upperdir", "workdir",
                                 "redirect_dir", "uuid", "userxattr"} and
             root_options["rw"] is None and root_options["userxattr"] is None and
             root_options["redirect_dir"] == "nofollow" and root_options["uuid"] == "on",
             "unrecognized overlay superblock options")
    _require(root_options["upperdir"] == overlay["UpperDir"] and
             root_options["workdir"] == overlay["WorkDir"], "overlay upper/work disagree with fresh inspect")
    lower = root_options["lowerdir"]
    _lower_layers(lower, graph_root, overlay["UpperDir"], "effective")
    # Inspected and effective lowerdirs may differ in spelling and layer count.
    # Upper/work correlation plus the caller's image/CID/runtime gates is the
    # origin assurance; neither a store prefix nor these strings prove ownership.
    return None
