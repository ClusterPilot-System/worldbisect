#!/usr/bin/env python3
"""Offline, age-encrypted backups of hub configuration, summaries, and audit data.

Linux/Python 3.10+ and an independently installed age executable are required.
Encryption authenticates ciphertext, not the identity of the backup producer.
"""
import argparse
import contextlib
import ctypes
from datetime import datetime, timedelta, timezone
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import resource
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import time


FORMAT = "worldbisect.hub-backup.v1"
DEFAULT_BYTES = 256 * 1024 * 1024
DEFAULT_FILES = 20000
MEMBER_BYTES = 16 * 1024 * 1024
MANIFEST_BYTES = 4 * 1024 * 1024
LOCKS = {"data": ".hub.lock", "audit": ".audit.lock"}


def absolute(path):
    if ".." in Path(path).parts:
        raise ValueError("parent traversal is not allowed in paths")
    return Path(os.path.abspath(path))


def directory(path, private=False):
    """Open every component without following symlinks; keep the final FD."""
    path = absolute(path)
    fd = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in path.parts[1:]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd)
            fd = child
        if private and os.fstat(fd).st_mode & 0o077:
            raise ValueError("data, audit and destination directories must be private (0700)")
        return fd
    except BaseException:
        os.close(fd)
        raise


def regular(fd):
    info = os.fstat(fd)
    if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_nlink != 1:
        raise ValueError("files must be private regular files (0600), without hard links")
    return info


def read_at(parent, name, limit):
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
    with os.fdopen(fd, "rb") as file:
        before = regular(file.fileno())
        if before.st_size > limit:
            raise ValueError("file exceeds backup member or remaining byte limit")
        content = file.read(limit + 1)
        after = os.fstat(file.fileno())
        if len(content) > limit or (before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise ValueError("file changed while reading or exceeded its limit")
        return content


def lock_at(parent, name):
    fd = os.open(name, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600, dir_fd=parent)
    try:
        regular(fd)
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return fd
    except BlockingIOError as error:
        os.close(fd)
        raise ValueError("hub is running: stop the server before backup") from error
    except BaseException:
        os.close(fd)
        raise


def add_member(archive, name, data):
    info = tarfile.TarInfo(name)
    info.size, info.mode = len(data), 0o600
    archive.addfile(info, io.BytesIO(data))


def collect(archive, manifest, directory_fd, prefix, limits):
    # Report/audit stores have shallow paths. Reject arbitrary recursive trees.
    if len(PurePosixPath(prefix).parts) > 3:
        raise ValueError("unexpected directory depth in hub data")
    names = []
    with os.scandir(directory_fd) as entries:
        for entry in entries:
            if len(names) >= limits["entries"]:
                raise ValueError("directory entry limit exceeded")
            names.append(entry.name)
    names.sort()
    for name in names:
        limits["entries"] -= 1
        if limits["entries"] < 0:
            raise ValueError("backup directory entry limit exceeded")
        if "/" in name or name in (".", ".."):
            raise ValueError("invalid directory entry")
        if prefix in LOCKS and name == LOCKS[prefix]:
            continue
        info = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
        full_name = prefix + "/" + name
        if stat.S_ISDIR(info.st_mode):
            child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory_fd)
            try:
                if os.fstat(child).st_mode & 0o077:
                    raise ValueError("hub directories must be private (0700)")
                collect(archive, manifest, child, full_name, limits)
            finally:
                os.close(child)
        elif stat.S_ISREG(info.st_mode):
            if limits["files"] <= 0:
                raise ValueError("backup file limit exceeded")
            content = read_at(directory_fd, name, min(MEMBER_BYTES, limits["bytes"]))
            add_member(archive, full_name, content)
            manifest[full_name] = {"bytes": len(content), "sha256": hashlib.sha256(content).hexdigest()}
            limits["bytes"] -= len(content)
            limits["files"] -= 1
        else:
            raise ValueError("symlinks and non-regular files are not accepted in backups")


def age_binary(path):
    binary = shutil.which(path)
    if not binary:
        raise ValueError("age is required; install and verify age from its upstream release or your distribution")
    return binary


def age_process(binary, args, source, destination, max_bytes):
    def constrain():
        resource.setrlimit(resource.RLIMIT_FSIZE, (max_bytes, max_bytes))
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))

    with source.open("rb") as input_file, destination.open("xb") as output_file:
        os.chmod(destination, 0o600)
        try:
            result = subprocess.run([binary, *args], stdin=input_file, stdout=output_file,
                                    stderr=subprocess.PIPE, timeout=180, check=False,
                                    preexec_fn=constrain, env={"PATH": os.environ.get("PATH", "/usr/bin:/bin")})
        except subprocess.TimeoutExpired as error:
            raise ValueError("age exceeded its 180 second time limit") from error
        if result.returncode:
            # Do not echo age stderr: identity paths or other private input may occur.
            raise ValueError("age failed: check identity/recipient, ciphertext integrity, and size limit")
        output_file.flush()
        os.fsync(output_file.fileno())


def backup(config, data, audit, output, recipient, age="age", max_bytes=DEFAULT_BYTES, max_files=DEFAULT_FILES):
    started = time.monotonic()
    config, data, audit, output = map(absolute, (config, data, audit, output))
    if not re.fullmatch(r"age1[0-9a-z]{58}", recipient):
        raise ValueError("recipient must be a native age public key (age1...)")
    if data == audit or data in audit.parents or audit in data.parents:
        raise ValueError("data and audit directories must be separate")
    if any(root == output.parent or root in output.parents for root in (data, audit)):
        raise ValueError("backup output must be outside the data and audit directories")
    binary = age_binary(age)
    with contextlib.ExitStack() as stack:
        def opened(path, private=False):
            fd = directory(path, private)
            stack.callback(os.close, fd)
            return fd
        output_fd = opened(output.parent, True)
        config_fd = opened(config.parent)
        roots = {"data": opened(data, True), "audit": opened(audit, True)}
        for name, fd in roots.items():
            stack.callback(os.close, lock_at(fd, LOCKS[name]))
        if os.path.lexists(output):
            raise ValueError("backup output already exists; refusing to overwrite")
        with tempfile.TemporaryDirectory(prefix=".hub-backup-", dir=f"/proc/self/fd/{output_fd}") as temp:
            temp = Path(temp)
            contents = read_at(config_fd, config.name, min(65536, max_bytes))
            manifest = {"config.json": {"bytes": len(contents), "sha256": hashlib.sha256(contents).hexdigest()}}
            limits = {"bytes": max_bytes - len(contents), "files": max_files - 1, "entries": max_files + 256}
            with tarfile.open(temp / "snapshot.tar", "w", format=tarfile.USTAR_FORMAT) as archive:
                add_member(archive, "config.json", contents)
                for name, fd in roots.items():
                    collect(archive, manifest, fd, name, limits)
                manifest_data = json.dumps({"format": FORMAT, "files": manifest}, sort_keys=True).encode()
                if len(manifest_data) > MANIFEST_BYTES:
                    raise ValueError("backup manifest exceeds its limit")
                add_member(archive, "manifest.json", manifest_data)
            encrypted = temp / "snapshot.age"
            age_process(binary, ["--encrypt", "--recipient", recipient], temp / "snapshot.tar", encrypted,
                        max_bytes + max_files * 1024 + MANIFEST_BYTES + 1024 * 1024)
            # link is atomic and never replaces an existing file, unlike rename.
            os.link(encrypted, output.name, dst_dir_fd=output_fd, follow_symlinks=False)
            os.fsync(output_fd)
            result = {"operation": "backup", "files": len(manifest), "plaintext_bytes": max_bytes - limits["bytes"],
                      "encrypted_bytes": encrypted.stat().st_size, "seconds": round(time.monotonic() - started, 3)}
        os.fsync(output_fd)
        return result


def safe_member(name):
    parts = PurePosixPath(name).parts
    if not parts or name.startswith("/") or "\\" in name or any(part in (".", "..") for part in name.split("/")):
        raise ValueError("unsafe backup member path")
    if name not in ("manifest.json", "config.json") and (parts[0] not in ("data", "audit") or len(parts) < 2 or len(parts) > 4):
        raise ValueError("unexpected backup member path")
    if name in ("data/.hub.lock", "audit/.audit.lock"):
        raise ValueError("backup must not contain process lock files")


def extract_verified(source, target, max_bytes, max_files):
    """No tar extraction API: validate each path/type and write new files only."""
    seen = {}
    manifest = None
    total = 0
    with tarfile.open(source, mode="r:") as archive:
        for member in archive:
            safe_member(member.name)
            if not member.isfile() or member.linkname or member.name in seen or member.name == "manifest.json" and manifest is not None:
                raise ValueError("backup contains duplicate, linked, or non-regular entries")
            limit = MANIFEST_BYTES if member.name == "manifest.json" else MEMBER_BYTES
            if member.size < 0 or member.size > limit:
                raise ValueError("backup member size limit exceeded")
            if member.name != "manifest.json" and (len(seen) >= max_files or total + member.size > max_bytes):
                raise ValueError("restored backup exceeds file or byte limit")
            content = archive.extractfile(member).read(member.size + 1)
            if len(content) != member.size:
                raise ValueError("truncated backup member")
            if member.name == "manifest.json":
                manifest = json.loads(content)
                continue
            destination = target / member.name
            parent = target
            for part in PurePosixPath(member.name).parts[:-1]:
                parent = parent / part
                parent.mkdir(mode=0o700, exist_ok=True)
            with destination.open("xb") as file:
                os.chmod(destination, 0o600)
                file.write(content)
                file.flush()
                os.fsync(file.fileno())
            seen[member.name] = {"bytes": len(content), "sha256": hashlib.sha256(content).hexdigest()}
            total += len(content)
    if not isinstance(manifest, dict) or manifest.get("format") != FORMAT or manifest.get("files") != seen or "config.json" not in seen:
        raise ValueError("backup manifest or content hashes do not match")
    (target / "data").mkdir(mode=0o700, exist_ok=True)
    (target / "audit").mkdir(mode=0o700, exist_ok=True)
    for root, _, _ in os.walk(target, topdown=False):
        # These paths were created under our private staging FD; /proc/self/fd
        # is an intentional internal descriptor path, not an external input.
        fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    return {"files": len(seen), "plaintext_bytes": total}


def publish_directory(source, parent_fd, name):
    # Linux renameat2(RENAME_NOREPLACE) publishes the whole verified snapshot
    # atomically without replacing even an empty pre-existing directory.
    libc = ctypes.CDLL(None, use_errno=True)
    rename = getattr(libc, "renameat2", None)
    if rename is None:
        raise ValueError("restore requires Linux renameat2 support")
    if rename(-100, os.fsencode(source), parent_fd, os.fsencode(name), 1) != 0:
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code))
    os.fsync(parent_fd)


def quarantine_ci(target):
    """A rolled-back replay ledger must not admit still-live CI credentials."""
    path = target / "config.json"
    if path.stat().st_size > 65536:
        raise ValueError("restored config exceeds 64 KiB")
    config = json.loads(path.read_bytes())
    if not isinstance(config, dict):
        raise ValueError("restored config must be a JSON object")
    publishers = config.get("ci_publishers")
    if not publishers:
        return None
    if not isinstance(publishers, list) or config.get("version") != 2:
        raise ValueError("restored CI policy configuration is invalid")
    deadline = datetime.now(timezone.utc) + timedelta(minutes=11)
    previous = config.get("ci_quarantine_until")
    if previous:
        if not isinstance(previous, str):
            raise ValueError("restored CI quarantine deadline is invalid")
        parsed = datetime.fromisoformat(previous.replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            raise ValueError("restored CI quarantine deadline requires a timezone")
        deadline = max(deadline, parsed.astimezone(timezone.utc))
    stamp = deadline.isoformat().replace("+00:00", "Z")
    config["ci_quarantine_until"] = stamp
    content = json.dumps(config, indent=2).encode() + b"\n"
    if len(content) > 65536:
        raise ValueError("restored config plus quarantine exceeds 64 KiB")
    with path.open("wb") as file:
        file.write(content)
        file.flush()
        os.fsync(file.fileno())
    return stamp


def restore(backup_path, identity, target, age="age", max_bytes=DEFAULT_BYTES, max_files=DEFAULT_FILES):
    started = time.monotonic()
    backup_path, identity, target = map(absolute, (backup_path, identity, target))
    binary = age_binary(age)
    with contextlib.ExitStack() as stack:
        target_fd = directory(target.parent, True)
        stack.callback(os.close, target_fd)
        if os.path.lexists(target):
            raise ValueError("restore target already exists; choose a new directory")
        # Open and inspect the identity without printing or putting key text in args.
        identity_parent = directory(identity.parent)
        stack.callback(os.close, identity_parent)
        identity_content = read_at(identity_parent, identity.name, 65536)
        # This tool supports native, unencrypted age identities only. No plugin,
        # SSH identity or passphrase interaction can execute during restoration.
        lines = [line.strip() for line in identity_content.decode("ascii").splitlines() if line.strip() and not line.lstrip().startswith("#")]
        if not lines or any(not re.fullmatch(r"AGE-SECRET-KEY-1[0-9A-Z]{58}", line) for line in lines):
            raise ValueError("identity must contain native age secret keys only")
        encrypted_parent = directory(backup_path.parent)
        stack.callback(os.close, encrypted_parent)
        encrypted_fd = os.open(backup_path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=encrypted_parent)
        stack.callback(os.close, encrypted_fd)
        info = regular(encrypted_fd)
        archive_limit = max_bytes + max_files * 1024 + MANIFEST_BYTES + 1024 * 1024
        if info.st_size > archive_limit:
            raise ValueError("encrypted backup exceeds its size limit")
        with tempfile.TemporaryDirectory(prefix=".hub-restore-", dir=f"/proc/self/fd/{target_fd}") as temp:
            temp = Path(temp)
            # Copy descriptor-opened inputs into this private staging directory.
            identity_copy = temp / "identity.txt"
            identity_copy.write_bytes(identity_content)
            os.chmod(identity_copy, 0o600)
            encrypted_copy = temp / "snapshot.age"
            with os.fdopen(os.dup(encrypted_fd), "rb") as file, encrypted_copy.open("xb") as output:
                os.chmod(encrypted_copy, 0o600)
                copied = 0
                while True:
                    chunk = file.read(min(1024 * 1024, archive_limit + 1 - copied))
                    if not chunk:
                        break
                    copied += len(chunk)
                    if copied > archive_limit:
                        raise ValueError("encrypted backup changed or exceeded its size limit")
                    output.write(chunk)
            # Pass a real staging path to the subprocess, not /proc/self/fd of
            # a parent-only descriptor. No plaintext is published before age
            # has successfully authenticated the complete ciphertext.
            identity_real = identity_copy.resolve()
            decrypted = temp / "snapshot.tar"
            age_process(binary, ["--decrypt", "--identity", str(identity_real)], encrypted_copy, decrypted, archive_limit)
            destination = temp / "restored"
            destination.mkdir(mode=0o700)
            result = extract_verified(decrypted, destination, max_bytes, max_files)
            quarantine = quarantine_ci(destination)
            if quarantine:
                result["ci_quarantine_until"] = quarantine
            publish_directory(destination, target_fd, target.name)
        os.fsync(target_fd)
        result.update(operation="restore", seconds=round(time.monotonic() - started, 3))
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--age", default="age", help="verified age executable (default: PATH lookup)")
    parser.add_argument("--max-bytes", type=int, default=DEFAULT_BYTES)
    parser.add_argument("--max-files", type=int, default=DEFAULT_FILES)
    commands = parser.add_subparsers(dest="command", required=True)
    create = commands.add_parser("backup", help="stop hub first; encrypt config, data, and audit together")
    for flag in ("config", "data", "audit", "output", "recipient"):
        create.add_argument("--" + flag, required=True)
    recover = commands.add_parser("restore", help="decrypt and verify into a new private directory")
    for flag in ("backup", "identity", "target"):
        recover.add_argument("--" + flag, required=True)
    args = parser.parse_args()
    if not 1 <= args.max_bytes <= 2 * 1024 ** 3 or not 1 <= args.max_files <= 100100:
        parser.error("limits must be 1..2 GiB and 1..100100 files")
    os.umask(0o077)
    try:
        options = {"age": args.age, "max_bytes": args.max_bytes, "max_files": args.max_files}
        result = backup(args.config, args.data, args.audit, args.output, args.recipient, **options) if args.command == "backup" else restore(args.backup, args.identity, args.target, **options)
        print(json.dumps(result, sort_keys=True))
    except (ValueError, OSError, tarfile.TarError, UnicodeError) as error:
        print(f"hub backup: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
