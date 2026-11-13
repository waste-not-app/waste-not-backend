import io
import base64
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

import bluegreen


class BlueGreenRollback(unittest.TestCase):
    def scenario(self, *, readiness=None, reloads=None, migration_error=False, public_ok=True, finalize=True):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / 'upstream.conf'
            original = b'# preserve operator comment\nupstream wastenot_upstream { server 127.0.0.1:18081; }\n'
            config.write_bytes(original)
            calls = []

            def run(*args):
                calls.append(args)
                if '--entrypoint' in args and migration_error:
                    raise subprocess.CalledProcessError(1, args)
                return 'sha256:test' if 'inspect' in args else ''

            values = {'DATABASE_URL_APP': 'private-runtime', 'DATABASE_URL_MIGRATE': 'private-migrate',
                      'JWT_SECRET_BASE64': 'private-jwt'}
            with patch.object(bluegreen, 'UPSTREAM', config), patch.object(bluegreen, 'PENDING', Path(directory) / 'pending.json'), patch.object(bluegreen, 'settings', return_value=values), \
                 patch.object(bluegreen, 'run', side_effect=run), \
                 patch.object(bluegreen, 'reconcile'), \
                 patch.object(bluegreen, 'ready', side_effect=readiness or (lambda *args, **kwargs: True)), \
                 patch.object(bluegreen, 'reload_nginx', side_effect=reloads) as reload, \
                 patch.object(bluegreen.subprocess, 'run', return_value=subprocess.CompletedProcess([],0)) as commands:
                error = None
                try:
                    image='wastenot-backend:' + 'a' * 40
                    bluegreen.deploy(image, Path('unused'))
                    if not public_ok:
                        bluegreen.rollback(image)
                        raise ValueError('Public HTTPS rejected by runner')
                    if finalize:
                        bluegreen.confirm(image)
                except (ValueError, subprocess.CalledProcessError) as exc:
                    error = exc
                return config.read_bytes(), original, calls, commands.call_args_list, reload.call_count, error

    def test_failed_nginx_validation_restores_exact_previous_config(self):
        raw, original, _, commands, reloads, error = self.scenario(reloads=[ValueError('nginx failed'), None])
        self.assertIsNotNone(error)
        self.assertEqual(raw, original)
        self.assertEqual(reloads, 2)
        self.assertFalse(any('stop' in call.args[0] and 'wastenot-api-blue' in call.args[0] for call in commands))

    def test_failed_public_route_restores_config_and_keeps_old_slot(self):
        raw, original, _, commands, _, error = self.scenario(public_ok=False)
        self.assertIsNotNone(error)
        self.assertEqual(raw, original)
        self.assertFalse(any('stop' in call.args[0] and 'wastenot-api-blue' in call.args[0] for call in commands))

    def test_failed_migration_does_not_touch_upstream_or_containers(self):
        raw, original, _, commands, reloads, error = self.scenario(migration_error=True)
        self.assertIsNotNone(error)
        self.assertEqual(raw, original)
        self.assertEqual(commands, [])
        self.assertEqual(reloads, 0)

    def test_public_confirmation_is_required_before_old_slot_stops(self):
        _, _, _, commands, _, error = self.scenario(finalize=False)
        self.assertIsNone(error)
        self.assertFalse(any('stop' in call.args[0] and 'wastenot-api-blue' in call.args[0] for call in commands))

    def test_wrong_run_cannot_confirm_or_roll_back_another_pending_image(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory) / 'pending.json'
            state.write_text('{"image":"wastenot-backend:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')
            with patch.object(bluegreen, 'PENDING', state), patch.object(bluegreen, 'drain') as drain:
                with self.assertRaises(ValueError):
                    bluegreen.confirm('wastenot-backend:' + 'b' * 40)
                with self.assertRaises(ValueError):
                    bluegreen.rollback('wastenot-backend:' + 'b' * 40)
                drain.assert_not_called()
                self.assertTrue(state.exists())

    def test_interrupted_confirmation_recovers_old_slot_before_restoring_traffic(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / 'upstream.conf'
            state = Path(directory) / 'pending.json'
            original = b'upstream wastenot_upstream { server 127.0.0.1:18081; }'
            current = b'upstream wastenot_upstream { server 127.0.0.1:18082; }'
            config.write_bytes(current)
            image = 'wastenot-backend:' + 'a' * 40
            state.write_text(json.dumps({'image': image, 'previous': 'wastenot-api-blue',
                                         'target': 'wastenot-api-green', 'port': 18082,
                                         'upstream': base64.b64encode(original).decode()}))
            events = []
            def run(*args):
                events.append(args)
                if args[:2] == ('docker', 'start'):
                    self.assertEqual(config.read_bytes(), current)
                return ''
            def ready(url, *args, **kwargs):
                events.append(url)
                if ':18081/' in url:
                    self.assertEqual(config.read_bytes(), current)
                return True
            with patch.object(bluegreen, 'UPSTREAM', config), patch.object(bluegreen, 'PENDING', state), \
                 patch.object(bluegreen, 'run', side_effect=run), patch.object(bluegreen, 'ready', side_effect=ready), \
                 patch.object(bluegreen.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)):
                bluegreen.rollback(image)
            self.assertEqual(events[0], ('docker', 'start', 'wastenot-api-blue'))
            self.assertEqual(config.read_bytes(), original)
            self.assertFalse(state.exists())

    def test_failed_old_slot_recovery_does_not_replace_working_route(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / 'upstream.conf'
            state = Path(directory) / 'pending.json'
            original = b'upstream wastenot_upstream { server 127.0.0.1:18081; }'
            current = b'upstream wastenot_upstream { server 127.0.0.1:18082; }'
            config.write_bytes(current)
            image = 'wastenot-backend:' + 'a' * 40
            state.write_text(json.dumps({'image': image, 'previous': 'wastenot-api-blue',
                                         'target': 'wastenot-api-green', 'port': 18082,
                                         'upstream': base64.b64encode(original).decode()}))
            with patch.object(bluegreen, 'UPSTREAM', config), patch.object(bluegreen, 'PENDING', state), \
                 patch.object(bluegreen, 'run'), patch.object(bluegreen, 'ready', return_value=False):
                with self.assertRaises(ValueError):
                    bluegreen.rollback(image)
            self.assertEqual(config.read_bytes(), current)
            self.assertTrue(state.exists())

    def test_failed_target_does_not_switch_route(self):
        raw, original, _, _, reloads, error = self.scenario(readiness=[False])
        self.assertIsNotNone(error)
        self.assertEqual(raw, original)
        self.assertEqual(reloads, 0)

    def test_success_limits_resources_and_drains_requests(self):
        raw, _, calls, commands, _, error = self.scenario()
        self.assertIsNone(error)
        self.assertIn(b'18082', raw)
        start = next(args for args in calls if '-d' in args)
        for flag in ('--read-only', '--cap-drop', '--security-opt', '--memory', '--cpus', '--pids-limit'):
            self.assertIn(flag, start)
        self.assertNotIn('private-jwt', repr(calls))
        self.assertIn(['docker', 'stop', '--time', '45', 'wastenot-api-blue'], [c.args[0] for c in commands])

    def test_rollback_reload_failure_keeps_new_slot_available(self):
        raw, original, _, commands, _, error = self.scenario(reloads=[ValueError('activate'), ValueError('restore')])
        self.assertIsNotNone(error)
        self.assertEqual(raw, original)
        self.assertEqual(len(commands), 2)  # Only drain/cleanup of the inactive slot before startup.

    def test_interrupted_switch_is_reconciled_before_either_container_is_removed(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / 'upstream.conf'
            config.write_text('upstream wastenot_upstream { server 127.0.0.1:18082; }')
            events = []
            def ready(url, release, **kwargs):
                events.append(url)
                return True
            def run(*args):
                if args[:2] == ('systemctl', 'reload'):
                    events.append('reload')
                return 'sha256:test' if 'inspect' in args else ''
            def command(args, **kwargs):
                events.append(args)
            values = {'DATABASE_URL_APP': 'db', 'DATABASE_URL_MIGRATE': 'db', 'JWT_SECRET_BASE64': 'jwt'}
            with patch.object(bluegreen, 'UPSTREAM', config), patch.object(bluegreen, 'PENDING', Path(directory) / 'pending.json'), patch.object(bluegreen, 'settings', return_value=values), \
                 patch.object(bluegreen, 'run', side_effect=run), patch.object(bluegreen, 'ready', side_effect=ready), \
                 patch.object(bluegreen.subprocess, 'run', side_effect=command):
                bluegreen.deploy('wastenot-backend:' + 'a' * 40, Path('unused'))
            reload_index = events.index('reload')
            drain_index = events.index(['docker', 'stop', '--time', '45', 'wastenot-api-blue'])
            remove_index = events.index(['docker', 'rm', '-f', 'wastenot-api-blue'])
            self.assertLess(reload_index, drain_index)
            self.assertLess(drain_index, remove_index)

    def test_unhealthy_persisted_slot_prevents_reconciliation(self):
        with patch.object(bluegreen, 'ready', return_value=False), patch.object(bluegreen, 'reload_nginx') as reload:
            with self.assertRaises(ValueError):
                bluegreen.reconcile(18082)
            reload.assert_not_called()

    def test_photo_storage_dir_mounts_volume_and_rejects_traversal(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / 'upstream.conf'
            config.write_text('upstream wastenot_upstream { server 127.0.0.1:18081; }')
            calls = []
            def run(*args):
                calls.append(args)
                return 'sha256:test' if 'inspect' in args else ''
            values = {'DATABASE_URL_APP': 'db', 'DATABASE_URL_MIGRATE': 'db', 'JWT_SECRET_BASE64': 'jwt',
                      'PHOTO_STORAGE_DIR': '/var/lib/wastenot-photos'}
            with patch.object(bluegreen, 'UPSTREAM', config), patch.object(bluegreen, 'PENDING', Path(directory) / 'pending.json'), \
                 patch.object(bluegreen, 'settings', return_value=values), patch.object(bluegreen, 'run', side_effect=run), \
                 patch.object(bluegreen, 'reconcile'), patch.object(bluegreen, 'ready', return_value=True), \
                 patch.object(bluegreen, 'reload_nginx'), patch.object(bluegreen.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)):
                bluegreen.deploy('wastenot-backend:' + 'a' * 40, Path('unused'))
            start = next(args for args in calls if '-d' in args)
            self.assertIn('--volume', start)
            self.assertIn('/var/lib/wastenot-photos:/var/lib/wastenot-photos', start)
            bad = dict(values, PHOTO_STORAGE_DIR='/var/lib/../etc/passwd')
            with patch.object(bluegreen, 'UPSTREAM', config), patch.object(bluegreen, 'PENDING', Path(directory) / 'pending2.json'), \
                 patch.object(bluegreen, 'settings', return_value=bad), patch.object(bluegreen, 'run', side_effect=run):
                with self.assertRaises(ValueError):
                    bluegreen.deploy('wastenot-backend:' + 'a' * 40, Path('unused'))


class ReadinessIdentity(unittest.TestCase):
    def test_wrong_release_cannot_satisfy_readiness(self):
        response = io.BytesIO(b'{"ready":true}')
        response.status = 200
        response.headers = {'X-WasteNot-Release': 'old'}
        with patch.object(bluegreen.urllib.request, 'urlopen', return_value=response), patch.object(bluegreen.time, 'sleep'):
            self.assertFalse(bluegreen.ready('https://example.invalid', 'new', attempts=1))


class SecretBoundary(unittest.TestCase):
    def test_readable_environment_is_rejected(self):
        path = SimpleNamespace(stat=lambda: SimpleNamespace(st_uid=0, st_mode=0o100644))
        with self.assertRaises(ValueError):
            bluegreen.settings(path)

    def test_configuration_is_data_and_not_shell_code(self):
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / 'must-not-exist'
            text = 'DATABASE_URL_APP=postgres://app\nDATABASE_URL_MIGRATE=postgres://migrate\nJWT_SECRET_BASE64=$(touch ' + str(marker) + ')\n'
            path = SimpleNamespace(stat=lambda: SimpleNamespace(st_uid=0, st_mode=0o100600), read_text=lambda: text)
            with self.assertRaises(ValueError):
                bluegreen.settings(path)
            self.assertFalse(marker.exists())


if __name__ == '__main__':
    unittest.main()
