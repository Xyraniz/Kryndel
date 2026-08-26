"""Portable Kryndel executable package format.

KEXE is deliberately small and deterministic. It is not an operating-system
PE/ELF binary; it is a self-contained Kryndel artifact executed by the Kryndel
runtime. A future native backend can keep this container contract while
replacing the payload with machine code.
"""

from __future__ import annotations

import hashlib
import struct
from pathlib import Path

from .bytecode import Module
from .contracts import strict_json_loads


MAGIC = b"KRYNEXE\x01"
HEADER = struct.Struct(">8sI32s")
MAX_PAYLOAD_BYTES = 16 * 1024 * 1024


class ArtifactError(ValueError):
    """Raised when a KEXE artifact is malformed or tampered with."""

    def __init__(self, message: str, code: str = "KRY6305") -> None:
        self.code = code
        super().__init__(f"{code} {message}")


def write_artifact(module: Module, path: str | Path) -> None:
    payload = module.dumps().encode("utf-8")
    if len(payload) > MAX_PAYLOAD_BYTES:
        raise ArtifactError(f"KEXE payload exceeds the {MAX_PAYLOAD_BYTES}-byte limit")
    target = Path(path)
    if target.is_symlink():
        raise ArtifactError("artifact output must not be a symlink", "KRY6301")
    digest = hashlib.sha256(payload).digest()
    target.write_bytes(HEADER.pack(MAGIC, len(payload), digest) + payload)


def read_artifact(path: str | Path) -> Module:
    artifact_path = Path(path)
    if artifact_path.is_symlink() or not artifact_path.is_file():
        raise ArtifactError("artifact must be a regular file", "KRY6301")
    try:
        file_size = artifact_path.stat().st_size
    except OSError as exc:
        raise ArtifactError(f"artifact cannot be inspected: {exc}", "KRY6301") from exc
    if file_size < HEADER.size:
        raise ArtifactError("artifact is shorter than the KEXE header")
    if file_size - HEADER.size > MAX_PAYLOAD_BYTES:
        raise ArtifactError(f"KEXE payload exceeds the {MAX_PAYLOAD_BYTES}-byte limit")
    try:
        data = artifact_path.read_bytes()
    except OSError as exc:
        raise ArtifactError(f"artifact cannot be read: {exc}", "KRY6301") from exc
    if len(data) != file_size:
        raise ArtifactError("artifact size changed while it was being read", "KRY6301")
    magic, length, digest = HEADER.unpack(data[: HEADER.size])
    if magic != MAGIC:
        raise ArtifactError("invalid KEXE magic")
    if length > MAX_PAYLOAD_BYTES:
        raise ArtifactError(f"KEXE payload exceeds the {MAX_PAYLOAD_BYTES}-byte limit")
    payload = data[HEADER.size :]
    if len(payload) != length:
        raise ArtifactError("KEXE payload length does not match its header")
    if hashlib.sha256(payload).digest() != digest:
        raise ArtifactError("KEXE checksum verification failed", "KRY6205")
    try:
        decoded = strict_json_loads(payload.decode("utf-8"), context="KEXE payload")
        return Module.from_dict(decoded)
    except (UnicodeDecodeError, ValueError, KeyError, TypeError) as exc:
        raise ArtifactError(f"invalid KEXE module: {exc}") from exc
