import io
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

import bluegreen


class BlueGreenRollback(unittest.TestCase):
    def scenario(self, *, readiness=None, reloads=None, migration_error=False):
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
            with patch.object(bluegreen, 'UPSTREAM', config), patch.object(bluegreen, 'settings', return_value=values), \
                 patch.object(bluegreen, 'run', side_effect=run), \
                 patch.object(bluegreen, 'reconcile'), \
                 patch.object(bluegreen, 'ready', side_effect=readiness or [True, True, True]), \
                 patch.object(bluegreen, 'reload_nginx', side_effect=reloads) as reload, \
                 patch.object(bluegreen.subprocess, 'run') as commands:
                error = None
                try:
                    bluegreen.deploy('wastenot-backend:' + 'a' * 40, Path('unused'))
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
        raw, original, _, commands, _, error = self.scenario(readiness=[True, True, False])
        self.assertIsNotNone(error)
        self.assertEqual(raw, original)
        self.assertFalse(any('stop' in call.args[0] and 'wastenot-api-blue' in call.args[0] for call in commands))

    def test_failed_migration_does_not_touch_upstream_or_containers(self):
        raw, original, _, commands, reloads, error = self.scenario(migration_error=True)
        self.assertIsNotNone(error)
        self.assertEqual(raw, original)
        self.assertEqual(commands, [])
        self.assertEqual(reloads, 0)

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
            with patch.object(bluegreen, 'UPSTREAM', config), patch.object(bluegreen, 'settings', return_value=values), \
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
