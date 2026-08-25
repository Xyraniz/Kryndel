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


MAGIC = b"KRYNEXE\x01"
HEADER = struct.Struct(">8sI32s")


class ArtifactError(ValueError):
    """Raised when a KEXE artifact is malformed or tampered with."""


def write_artifact(module: Module, path: str | Path) -> None:
    payload = module.dumps().encode("utf-8")
    digest = hashlib.sha256(payload).digest()
    Path(path).write_bytes(HEADER.pack(MAGIC, len(payload), digest) + payload)


def read_artifact(path: str | Path) -> Module:
    data = Path(path).read_bytes()
    if len(data) < HEADER.size:
        raise ArtifactError("artifact is shorter than the KEXE header")
    magic, length, digest = HEADER.unpack(data[: HEADER.size])
    if magic != MAGIC:
        raise ArtifactError("invalid KEXE magic")
    payload = data[HEADER.size :]
    if len(payload) != length:
        raise ArtifactError("KEXE payload length does not match its header")
    if hashlib.sha256(payload).digest() != digest:
        raise ArtifactError("KEXE checksum verification failed")
    try:
        return Module.from_dict(__import__("json").loads(payload.decode("utf-8")))
    except (UnicodeDecodeError, ValueError, KeyError, TypeError) as exc:
        raise ArtifactError(f"invalid KEXE module: {exc}") from exc
