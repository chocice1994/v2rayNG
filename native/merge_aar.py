#!/usr/bin/env python3
"""Merge sequential gomobile ABI outputs only when all common contents agree."""
import argparse
import io
from pathlib import Path, PurePosixPath
import zipfile

ABIS = {'armeabi-v7a', 'arm64-v8a', 'x86', 'x86_64'}


def read_entries(data):
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        names = [name for name in archive.namelist() if not name.endswith('/')]
        if len(names) != len(set(names)):
            raise ValueError('Duplicate archive entry')
        if archive.testzip() is not None:
            raise ValueError('Archive CRC error')
        result = {}
        for name in names:
            path = PurePosixPath(name)
            if path.is_absolute() or '..' in path.parts:
                raise ValueError('Unsafe archive entry')
            result[name] = archive.read(name)
        return result


def equivalent(name, left, right):
    # Jar container timestamps are not code changes. Every uncompressed member
    # must still have the exact same name and bytes, including manifests.
    if name.endswith('.jar'):
        return read_entries(left) == read_entries(right)
    return left == right


def merge(output, inputs):
    output = Path(output)
    if output.exists():
        raise ValueError('Refusing to overwrite existing output')
    common = None
    native = {}
    for path in inputs:
        entries = read_entries(Path(path).read_bytes())
        shared = {name: data for name, data in entries.items() if not name.startswith('jni/')}
        if common is None:
            common = shared
        elif set(common) != set(shared) or any(not equivalent(name, common[name], shared[name]) for name in common):
            raise ValueError('Non-JNI contents differ; refusing to combine runtimes')
        for name, data in entries.items():
            if name.startswith('jni/'):
                parts = PurePosixPath(name).parts
                if len(parts) != 3 or parts[1] not in ABIS or parts[2] != 'libgojni.so' or not data:
                    raise ValueError('Unexpected or empty native library')
                if name in native:
                    raise ValueError('Duplicate native ABI')
                native[name] = data
    expected = {f'jni/{abi}/libgojni.so' for abi in ABIS}
    if set(native) != expected:
        raise ValueError('All four ABIs are required')
    if common is None or 'classes.jar' not in common:
        raise ValueError('Missing common Java bindings')
    with zipfile.ZipFile(output, 'x', zipfile.ZIP_DEFLATED) as archive:
        for name, data in sorted((common | native).items()):
            archive.writestr(name, data)
    read_entries(output.read_bytes())
    print('Merged one AAR: identical common bindings and all four native ABIs')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output')
    parser.add_argument('inputs', nargs=4)
    args = parser.parse_args()
    merge(args.output, args.inputs)
