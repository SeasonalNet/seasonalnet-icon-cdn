"""Assemble a shell-free runtime from the pinned build distribution."""
from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess
import sysconfig


def python_runtime_file(relative: Path) -> bool:
    excluded = {'__pycache__', 'test', 'tests', 'idlelib', 'tkinter', 'turtledemo', 'ensurepip',
                'pip', 'setuptools', 'wheel', '_distutils_hack', 'pkg_resources', 'distutils-precedence.pth'}
    if any(part.startswith('config-') for part in relative.parts):
        return False
    if relative.name.startswith(('_tkinter.', '_test', '_ctypes_test.')):
        return False
    return not any(part in excluded or part.startswith(('pip-', 'setuptools-', 'wheel-')) and part.endswith('.dist-info') for part in relative.parts)


def linked_libraries(source: Path) -> list[Path]:
    result = subprocess.run(['ldd', str(source)], check=True, capture_output=True, text=True)
    if 'not found' in result.stdout:
        raise RuntimeError(result.stdout)
    return [Path(path) for path in re.findall(r'(?:=>\s*)?(/[^\s()]+)', result.stdout)]


def assemble(destination: Path, profile: str) -> None:
    destination.mkdir(parents=True, exist_ok=True)
    copied: set[str] = set()

    def copy_file(source: Path) -> None:
        key = str(source)
        if key in copied:
            return
        copied.add(key)
        target = destination / source.relative_to('/')
        target.parent.mkdir(parents=True, exist_ok=True)
        if source.is_symlink():
            resolved = source.resolve(strict=True)
            target.symlink_to(str(resolved))
            if resolved.is_dir():
                (destination / resolved.relative_to('/')).mkdir(parents=True, exist_ok=True)
                tree(resolved)
            else:
                copy_file(resolved)
        else:
            shutil.copy2(source, target)

    def tree(source: Path) -> None:
        if source.is_file() or source.is_symlink():
            copy_file(source)
        elif source.exists():
            (destination / source.relative_to('/')).mkdir(parents=True, exist_ok=True)
            for item in source.rglob('*'):
                if item.is_file() or item.is_symlink() and not item.is_dir():
                    copy_file(item)

    for source in ['/etc/ssl/certs', '/etc/ssl/openssl.cnf', '/etc/ssl/openssl.cnf.d', '/usr/lib/ssl/cert.pem', '/usr/lib/ssl/certs', '/usr/lib/ssl/openssl.cnf', '/etc/nsswitch.conf', '/etc/localtime', '/etc/os-release', '/usr/share/zoneinfo']:
        tree(Path(source))
    etc = destination / 'etc'
    etc.mkdir(exist_ok=True)
    (etc / 'passwd').write_text('service:x:10001:10001:service:/nonexistent:/sbin/nologin\n')
    (etc / 'group').write_text('service:x:10001:\n')
    tmp = destination / 'tmp'
    tmp.mkdir(exist_ok=True)
    tmp.chmod(0o1777)

    if profile == 'python':
        copy_file(Path('/usr/local/bin/python3.15'))
        stdlib = Path('/usr/local/lib/python3.15')
        for item in stdlib.rglob('*'):
            relative = item.relative_to(stdlib)
            if not python_runtime_file(relative):
                continue
            if item.is_file() or item.is_symlink() and not item.is_dir():
                copy_file(item)
        # Include shared libpython, native-extension dependencies, and libc's
        # optional NSS providers. Fail on unresolved ELF dependencies.
        candidates = [Path('/usr/local/bin/python3.15')]
        candidates.extend(item for item in (destination / 'usr/local/lib').rglob('*') if item.is_file() and '.so' in item.name)
        for library in Path('/usr/local/lib').glob('libpython3.15.so*'):
            copy_file(library)
        for library in Path('/usr/lib').rglob('libnss_*.so*'):
            copy_file(library)
        for item in candidates:
            source = Path('/') / item.relative_to(destination) if item.is_relative_to(destination) else item
            for library in linked_libraries(source):
                copy_file(library)
    else:
        # Keep the linked-library closure plus libvips/codec and charset/TLS
        # modules loaded at runtime. Do not copy unrelated distro libraries.
        library_root = Path('/usr/lib') / sysconfig.get_config_var('MULTIARCH')
        modules = []
        for pattern in ['vips-modules-*', 'ImageMagick-*', 'libheif', 'gconv', 'ossl-modules', 'engines-*']:
            for directory in library_root.glob(pattern):
                modules.extend(item for item in directory.rglob('*') if (item.is_file() or item.is_symlink()) and '.so' in item.name)
        for module in modules:
            copy_file(module)
        for source in [Path('/usr/local/bin/icon-cdn'), *modules]:
            for library in linked_libraries(source):
                copy_file(library)
        for base in ['/etc/fonts', '/usr/share/fonts', '/usr/share/fontconfig', '/usr/share/mime', '/usr/share/glib-2.0/schemas']:
            tree(Path(base))
        for state in ['var/cache/seasonalnet-icon-cdn']:
            directory = destination / state
            directory.mkdir(parents=True, exist_ok=True)
            os.chown(directory, 10001, 10001)
    # Keep source-package metadata for retained distro files so scanners can
    # identify native-library versions even though dpkg itself is not shipped.
    package_paths = copied | {str(Path(path).resolve()) for path in copied}
    ownership = subprocess.run(['dpkg-query', '-S', *sorted(package_paths)], capture_output=True, text=True)
    packages = {line.split(': ', 1)[0] for line in ownership.stdout.splitlines() if ': /' in line and not line.startswith('diversion')}
    retained = set()
    for package in packages:
        retained.add(package.split(':', 1)[0])
    status = Path('/var/lib/dpkg/status').read_text()
    blocks = [block for block in status.split('\n\n') if any(line.startswith('Package: ') and line[9:] in retained for line in block.splitlines())]
    if not blocks:
        raise RuntimeError('could not identify source packages for retained native files')
    metadata = destination / 'var/lib/dpkg'
    metadata.mkdir(parents=True, exist_ok=True)
    (metadata / 'status').write_text('\n\n'.join(blocks) + '\n')
    forbidden = {'sh', 'bash', 'dash', 'busybox', 'apt', 'apt-get', 'dpkg', 'curl', 'wget', 'pip', 'pip3'}
    for item in destination.rglob('*'):
        if item.name in forbidden and (item.is_file() or item.is_symlink()):
            raise RuntimeError(f'forbidden runtime tool: {item}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('profile', choices=['python', 'vips'])
    parser.add_argument('destination', type=Path)
    args = parser.parse_args()
    assemble(args.destination, args.profile)
