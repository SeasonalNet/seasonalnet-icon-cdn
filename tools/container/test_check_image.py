"""Failure-path tests for the exported-image security gate."""
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('image_check', Path(__file__).with_name('check-image.py'))
image_check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(image_check)

PROGRAM = 'usr/local/bin/icon-cdn'


class ImageCheckTests(unittest.TestCase):
    def run_check(self, extra=(), user='10001:10001', health=None, export_status=0):
        stream = io.BytesIO()
        entries = [(PROGRAM, 0, 0o755, b'\x7fELFtest'), *extra]
        with tarfile.open(fileobj=stream, mode='w') as archive:
            for name, owner, mode, data in entries:
                entry = tarfile.TarInfo(name)
                entry.uid = owner
                entry.gid = owner
                entry.mode = mode
                entry.size = len(data)
                archive.addfile(entry, io.BytesIO(data))
        stream.seek(0)
        metadata = [{'Id': 'fixture-id', 'Size': 100, 'Config': {'User': user, 'Healthcheck': {'Test': health or []}}}]
        def command(*args):
            return json.dumps(metadata) if args[1:3] == ('image', 'inspect') else 'fixture-container'
        with patch.object(image_check, 'command', side_effect=command), patch.object(image_check.subprocess, 'Popen') as export, patch.object(image_check.subprocess, 'run') as cleanup:
            export.return_value.stdout = stream
            export.return_value.wait.return_value = export_status
            try:
                return image_check.check('fixture-image')
            finally:
                if user in ('0', '0:0', 'root') or health and health[0] == 'CMD-SHELL':
                    cleanup.assert_not_called()
                else:
                    cleanup.assert_called_once()

    def test_minimal_image_passes(self):
        self.assertEqual(self.run_check()['result'], 'passed')

    def test_root_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'non-root'):
            self.run_check(user='0:0')

    def test_shell_healthcheck_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'shell'):
            self.run_check(health=['CMD-SHELL', 'true'])

    def test_shell_payload_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'forbidden utility'):
            self.run_check(extra=[('bin/sh', 0, 0o755, b'ELF')])

    def test_unexpected_executable_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'unexpected executable'):
            self.run_check(extra=[('usr/bin/helper', 0, 0o755, b'\x7fELFtest')])

    def test_service_owned_code_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'root-owned'):
            self.run_check(extra=[('app/server.js', 10001, 0o644, b'code')])

    def test_writable_code_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'group/world writes'):
            self.run_check(extra=[('app/server.js', 0, 0o666, b'code')])

    def test_failed_export_is_rejected(self):
        with self.assertRaisesRegex(RuntimeError, 'export failed'):
            self.run_check(export_status=1)


if __name__ == '__main__':
    unittest.main()
