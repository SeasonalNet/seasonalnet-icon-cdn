"""Check a built production image without installing tools inside it.

Usage: python3 tools/container/check-image.py IMAGE
"""
from __future__ import annotations

import json
from pathlib import PurePosixPath
import subprocess
import sys
import tarfile


def command(*arguments: str) -> str:
    return subprocess.check_output(arguments, text=True)


def check(image: str) -> dict[str, object]:
    metadata = json.loads(command('docker', 'image', 'inspect', image))[0]
    config = metadata['Config']
    user = config.get('User', '').split(':')[0]
    if user in ('', '0', 'root'):
        raise ValueError('production image must select a non-root user')
    health = config.get('Healthcheck', {}).get('Test', [])
    if health and health[0] == 'CMD-SHELL':
        raise ValueError('production healthcheck must not depend on a shell')
    programs = {'usr/local/bin/icon-cdn'}
    forbidden = {'sh', 'bash', 'dash', 'ash', 'busybox', 'apt', 'apt-get', 'dpkg',
                 'apk', 'rpm', 'yum', 'dnf', 'curl', 'wget', 'nc', 'ncat', 'netcat',
                 'ssh', 'scp', 'git', 'gcc', 'cc', 'g++', 'make', 'npm', 'npx',
                 'pnpm', 'corepack', 'pip', 'pip3'}
    findings: list[str] = []
    executables: list[str] = []
    count = 0
    container = command('docker', 'create', '--entrypoint', '/nonexistent', image).strip()
    try:
        process = subprocess.Popen(['docker', 'export', container], stdout=subprocess.PIPE)
        if process.stdout is None:
            raise RuntimeError('could not read image filesystem')
        with tarfile.open(fileobj=process.stdout, mode='r|') as archive:
            for member in archive:
                count += 1
                name = member.name.removeprefix('./').lstrip('/')
                path = PurePosixPath(name)
                if path.parent.as_posix() in ('bin', 'sbin', 'usr/bin', 'usr/sbin', 'usr/local/bin') and path.name in forbidden:
                    findings.append(f'forbidden utility: /{name}')
                if name.startswith(('usr/local/lib/node_modules/', 'app/node_modules/node-pg-migrate/', 'app/dist/tests/', 'app/dist/scripts/')):
                    findings.append(f'non-service payload: /{name}')
                application = name in ('app', 'opt/seasonal-astrocomd') or name.startswith(('app/', 'opt/seasonal-astrocomd/'))
                cache = name.endswith('/.next/cache') or '/.next/cache/' in name
                if application and not cache:
                    if member.uid != 0 or member.gid != 0:
                        findings.append(f'application entry is not root-owned: /{name}')
                    if not member.issym() and member.mode & 0o022:
                        findings.append(f'application entry permits group/world writes: /{name}')
                if not member.isfile():
                    continue
                contents = archive.extractfile(member)
                if contents is None or contents.read(4) != b'\x7fELF':
                    continue
                if name in programs and not member.mode & 0o111:
                    findings.append(f'runtime executable has no execute permission: /{name}')
                if name not in programs and '.so' not in path.name and not path.name.endswith('.node'):
                    findings.append(f'unexpected executable: /{name}')
                else:
                    executables.append('/' + name)
        process.stdout.close()
        if process.wait() != 0:
            raise RuntimeError('docker export failed')
    finally:
        subprocess.run(['docker', 'rm', container], check=True, stdout=subprocess.DEVNULL)
    if not any(path.lstrip('/') in programs for path in executables):
        findings.append('required runtime executable is missing')
    if findings:
        raise ValueError('\n'.join(findings))
    return {'image': image, 'image_id': metadata['Id'], 'size_bytes': metadata['Size'],
            'user': config['User'], 'filesystem_entries': count,
            'elf_executables_and_libraries': sorted(executables), 'result': 'passed'}


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    print(json.dumps(check(sys.argv[1]), indent=2))
