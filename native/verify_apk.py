#!/usr/bin/env python3
"""Verify every declared ABI contains Xray and both HEV native artifacts."""
import argparse
import zipfile

ABIS = ('armeabi-v7a', 'arm64-v8a', 'x86', 'x86_64')
LIBRARIES = ('libgojni.so', 'libhev-socks5-tunnel.so', 'libhevsockstun.so')


def verify(path):
    with zipfile.ZipFile(path) as archive:
        names = set(archive.namelist())
        for abi in ABIS:
            for library in LIBRARIES:
                member = f'lib/{abi}/{library}'
                if member not in names:
                    raise ValueError(f'{path}: missing {member}')
                if archive.getinfo(member).file_size == 0:
                    raise ValueError(f'{path}: empty {member}')
    print(f'{path}: all four ABIs contain all three native libraries')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('apks', nargs='+')
    for path in parser.parse_args().apks:
        verify(path)
