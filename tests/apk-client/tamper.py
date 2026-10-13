"""Corrupt signed legacy APK archives without introducing framing/size errors."""

import gzip
import io
from pathlib import Path
import sys
import tarfile
import zlib


def member(data):
    stream = zlib.decompressobj(31)
    fragment = stream.decompress(data)
    if not stream.eof:
        raise ValueError("Incomplete gzip member")
    return fragment, data[:len(data) - len(stream.unused_data)], stream.unused_data


def padded_gzip(fragment, size):
    compressed = gzip.compress(fragment, compresslevel=9, mtime=0)
    padding = size - len(compressed)
    if padding < 0:
        return None
    header = bytearray(compressed[:10])
    if padding:
        # FCOMMENT is an optional zero-terminated string of any byte length.
        header[3] |= 16
        extra = b"X" * (padding - 1) + b"\0"
    else:
        extra = b""
    return bytes(header) + extra + compressed[10:]


def archive_entry(fragment, predicate):
    # Abuild emits PAX extension headers before the actual signature/PKGINFO.
    # Parse them rather than assuming the first 512-byte header is the file.
    with tarfile.open(fileobj=io.BytesIO(fragment), mode="r:") as archive:
        entries = [entry for entry in archive if entry.isfile() and predicate(entry.name)]
    if len(entries) != 1 or entries[0].size <= 0:
        raise ValueError("Expected exactly one nonempty fixture archive entry")
    return entries[0]


def tamper_signature(data):
    fragment, _, remainder = member(data)
    signature = bytearray(fragment)
    entry = archive_entry(fragment, lambda name: name.startswith(".SIGN."))
    signature[entry.offset_data] ^= 1
    return gzip.compress(bytes(signature), mtime=0) + remainder


def tamper_package(data):
    signature, original_signature, rest = member(data)
    archive_entry(signature, lambda name: name.startswith(".SIGN."))
    control, original_control, payload = member(rest)
    metadata = archive_entry(control, lambda name: name == ".PKGINFO")
    description = b"Local signed APK cache fixture"
    relative_offset = control[metadata.offset_data:metadata.offset_data + metadata.size].find(description)
    if relative_offset < 0:
        raise ValueError("Expected fixture control metadata not found")
    offset = metadata.offset_data + relative_offset
    # Change one signed description byte, keeping valid metadata and the
    # original signature/data members. Retain the trusted index's exact size.
    for position in range(offset, offset + len(description)):
        for value in b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789":
            if control[position] == value:
                continue
            changed = control[:position] + bytes([value]) + control[position + 1:]
            replacement = padded_gzip(changed, len(original_control))
            if replacement is not None:
                result = original_signature + replacement + payload
                assert len(result) == len(data)
                return result
    raise ValueError("Could not preserve the original compressed control size")


if __name__ == "__main__":
    mode, filename = sys.argv[1:]
    path = Path(filename)
    operation = {"signature": tamper_signature, "package": tamper_package}[mode]
    path.write_bytes(operation(path.read_bytes()))
