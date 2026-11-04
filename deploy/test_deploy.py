import hashlib
import io
import json
from pathlib import Path
import tarfile
import unittest
import tempfile
import subprocess
from unittest.mock import patch

import deploy


def release(changes=None, extra=None):
    elf = b'\x7fELF\x02' + bytes(13) + b'\x3e\x00' + bytes(100)
    files = {name: elf for name in ('api', 'migrate', 'guides')}
    files['release.json'] = json.dumps({'commit': 'a' * 40, 'sha256': {
        name: hashlib.sha256(raw).hexdigest() for name, raw in files.items()}}).encode()
    files.update(changes or {})
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w', format=tarfile.USTAR_FORMAT) as archive:
        for name, raw in files.items():
            entry = tarfile.TarInfo(name)
            entry.size = len(raw)
            archive.addfile(entry, io.BytesIO(raw))
        if extra is not None:
            archive.addfile(extra)
    return stream.getvalue()


class ReleaseBoundary(unittest.TestCase):
    def test_valid(self):
        commit, files = deploy.parse_release(release())
        self.assertEqual(commit, 'a' * 40)
        self.assertEqual(set(files), deploy.NAMES)

    def test_tampered_binary(self):
        with self.assertRaises(ValueError):
            deploy.parse_release(release({'api': b'tampered'}))

    def test_path_escape(self):
        with self.assertRaises(ValueError):
            deploy.parse_release(release(extra=tarfile.TarInfo('../../etc/passwd')))

    def test_symlink(self):
        entry = tarfile.TarInfo('api')
        entry.type = tarfile.SYMTYPE
        entry.linkname = '/bin/sh'
        with self.assertRaises(ValueError):
            deploy.parse_release(release(extra=entry))

    def test_duplicate_member(self):
        with self.assertRaises(ValueError):
            deploy.parse_release(release(extra=tarfile.TarInfo('api')))

    def test_compressed_input_rejected(self):
        import gzip
        with self.assertRaises(tarfile.ReadError):
            deploy.parse_release(gzip.compress(release()))

    def test_archive_limit(self):
        with patch.object(deploy, 'MAX_ARCHIVE', 10):
            with self.assertRaises(ValueError):
                deploy.parse_release(release())


class DeploymentRollback(unittest.TestCase):
    def test_failed_readiness_restores_previous_binary(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            (root/'releases').mkdir()
            previous=root/'releases'/('b'*40)
            previous.mkdir()
            (root/'current').symlink_to(previous)
            with patch.object(deploy,'ROOT',root), patch.object(deploy,'environment',return_value={}), \
                 patch.object(deploy.subprocess,'run',return_value=subprocess.CompletedProcess([],0)), \
                 patch.object(deploy,'activate') as activate, patch.object(deploy,'ready',return_value=False):
                with self.assertRaises(ValueError):
                    deploy.deploy(release())
                self.assertEqual([c.args[0] for c in activate.call_args_list],[root/'releases'/('a'*40),previous])

    def test_failed_migration_preserves_running_release(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            (root/'releases').mkdir()
            with patch.object(deploy,'ROOT',root), patch.object(deploy,'environment',return_value={}), \
                 patch.object(deploy.subprocess,'run',return_value=subprocess.CompletedProcess([],1)), \
                 patch.object(deploy,'activate') as activate:
                with self.assertRaises(ValueError):
                    deploy.deploy(release())
                activate.assert_not_called()


class ReadinessScope(unittest.TestCase):
    def test_prefixed_route_is_required_when_installed(self):
        class Response(io.BytesIO):
            status=200
        def response(request,timeout):
            if request.full_url.startswith('http://127.0.0.1:18080/'):
                return Response(b'{"ready":true}')
            raise OSError('prefix unavailable')
        with patch.object(deploy.Path,'exists',return_value=True), \
             patch.object(deploy.urllib.request,'urlopen',side_effect=response), \
             patch.object(deploy.time,'sleep'):
            self.assertFalse(deploy.ready())


if __name__ == '__main__':
    unittest.main()
