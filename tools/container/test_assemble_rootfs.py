"""Test dependency discovery and exclusions used by the runtime assembler."""
import importlib.util
from pathlib import Path
import subprocess
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('rootfs', Path(__file__).with_name('assemble-rootfs.py'))
rootfs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rootfs)


class RootfsTests(unittest.TestCase):
    def test_required_python_payload_is_retained(self):
        for name in ['lib-dynload/_ssl.cpython-315.so', 'site-packages/PIL/Image.py', 'site-packages/Pillow.libs/libjpeg.so']:
            with self.subTest(name=name):
                self.assertTrue(rootfs.python_runtime_file(Path(name)))

    def test_unused_tools_gui_and_startup_hook_are_excluded(self):
        for name in ['config-3.15-x86_64-linux-gnu/python.o', 'site-packages/pip/__init__.py', 'site-packages/setuptools-83.0.dist-info/METADATA', 'site-packages/_distutils_hack/__init__.py', 'site-packages/distutils-precedence.pth', 'lib-dynload/_tkinter.cpython-315.so', 'lib-dynload/_testcapi.cpython-315.so']:
            with self.subTest(name=name):
                self.assertFalse(rootfs.python_runtime_file(Path(name)))

    def test_linker_paths_include_loader_and_transitive_libraries(self):
        output = 'linux-vdso.so.1 (0x123)\nlibexample.so => /lib/example.so (0x456)\n/lib64/ld-linux.so.2 (0x789)\n'
        with patch.object(rootfs.subprocess, 'run', return_value=SimpleNamespace(stdout=output)) as run:
            self.assertEqual(rootfs.linked_libraries(Path('/app/program')), [Path('/lib/example.so'), Path('/lib64/ld-linux.so.2')])
            self.assertTrue(run.call_args.kwargs['check'])

    def test_unresolved_library_fails_closed(self):
        with patch.object(rootfs.subprocess, 'run', return_value=SimpleNamespace(stdout='libmissing.so => not found')):
            with self.assertRaisesRegex(RuntimeError, 'not found'):
                rootfs.linked_libraries(Path('/app/program'))

    def test_linker_failure_is_not_ignored(self):
        with patch.object(rootfs.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'ldd')):
            with self.assertRaises(subprocess.CalledProcessError):
                rootfs.linked_libraries(Path('/app/program'))


if __name__ == '__main__':
    unittest.main()
