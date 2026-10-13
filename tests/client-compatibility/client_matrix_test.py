#!/usr/bin/env python3
"""Fast regressions for the release gate; no Docker or repository access."""
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import client_matrix as matrix

SHA = 'a' * 40
PROOF = {'first_origin_package_gets': 1, 'second_origin_package_gets': 1,
         'client_cache_cleared': True, 'payload_matches': True}


class MatrixTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.rows = matrix.clients('apt')[:2]

    def write_report(self, row, attempt=1, **updates):
        report = {**row, 'source_sha': SHA, 'run_id': '123', 'run_attempt': attempt,
                  'status': 'passed', 'cache_hit': dict(PROOF), **updates}
        path = self.root / f"{row['id']}-{attempt}" / 'report.json'
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(report))
        return path

    def collect(self):
        return matrix.collect_reports(self.rows, self.root, SHA, '123')

    def test_family_selection_is_allowlisted(self):
        self.assertTrue(matrix.clients())
        self.assertEqual({row['family'] for row in matrix.clients()}, set(matrix.FAMILIES))
        for family in matrix.FAMILIES:
            self.assertEqual({row['family'] for row in matrix.clients(family)}, {family})
        with self.assertRaises(ValueError):
            matrix.clients('apt; echo injected')

    def test_empty_and_partial_evidence_fail(self):
        self.assertFalse(self.collect()['passed'])
        self.write_report(self.rows[0])
        report = self.collect()
        self.assertFalse(report['passed'])
        self.assertEqual(report['clients'][0]['status'], 'passed')
        self.assertEqual(report['clients'][1]['status'], 'failed')

    def test_stale_commit_and_other_run_cannot_pass(self):
        self.write_report(self.rows[0], source_sha='b' * 40)
        self.write_report(self.rows[1], run_id='other')
        self.assertTrue(all(row['status'] == 'failed' for row in self.collect()['clients']))

    def test_failed_job_rerun_keeps_previous_successful_jobs(self):
        self.write_report(self.rows[0])
        self.write_report(self.rows[1], status='failed', error='bootstrap failed')
        self.write_report(self.rows[1], 2)
        report = self.collect()
        self.assertTrue(report['passed'])
        self.assertEqual([row['run_attempt'] for row in report['clients']], [1, 2])

    def test_latest_failed_attempt_overrides_old_success(self):
        for row in self.rows:
            self.write_report(row)
        self.write_report(self.rows[0], 2, status='failed', error='signature accepted')
        self.assertFalse(self.collect()['passed'])

    def test_failed_status_with_empty_error_cannot_pass(self):
        for error in ('', None, False):
            for row in self.rows:
                self.write_report(row, status='failed', error=error, cache_hit={})
            self.assertFalse(self.collect()['passed'])
            self.assertTrue(all(row['status'] == 'failed' for row in self.collect()['clients']))

    def test_new_attempt_job_failure_without_report_cannot_reuse_old_pass(self):
        for row in self.rows:
            self.write_report(row)
        for result in ('failure', 'cancelled', 'skipped'):
            report = matrix.collect_reports(self.rows, self.root, SHA, '123', result)
            self.assertFalse(report['passed'])

    def test_manifest_mismatch_and_duplicate_reports_fail_closed(self):
        path = self.write_report(self.rows[0])
        self.write_report(self.rows[1], image='untrusted:latest')
        other = self.root / 'duplicate' / 'report.json'
        other.parent.mkdir()
        other.write_text(path.read_text())
        self.assertFalse(self.collect()['passed'])
        self.assertEqual(len(self.collect()['invalid_reports']), 2)

    def test_report_success_requires_verified_cache_hit(self):
        for row in self.rows:
            self.write_report(row, cache_hit={**PROOF, 'second_origin_package_gets': 2})
        self.assertFalse(self.collect()['passed'])
        for updates in ({'first_origin_package_gets': 0}, {'second_origin_package_gets': True},
                        {'client_cache_cleared': False}, {'payload_matches': False}):
            with self.subTest(updates=updates), self.assertRaises(ValueError):
                matrix.cache_proof({**PROOF, **updates})

    def fake_client(self, status=0, stamp=True, proof=PROOF):
        def launch(_command, **kwargs):
            output = Path(kwargs['env']['APT_CLIENT_EVIDENCE_DIR']) / 'fixture-image'
            output.mkdir(parents=True)
            if stamp:
                (output / 'result.txt').write_text('PASS: signed install\n')
                (output / 'cache-hit.json').write_text(json.dumps(proof))
            class Process:
                stdout = io.StringIO('client log\n')
                def wait(self):
                    return status
            return Process()
        return launch

    def run_stub(self, **kwargs):
        row = self.rows[0]
        with patch.object(matrix, 'source_sha', return_value=SHA), \
             patch.object(matrix.subprocess, 'Popen', side_effect=self.fake_client(**kwargs)):
            result = matrix.run_client(row['id'], self.root)
        report = json.loads((self.root / row['id'] / 'report.json').read_text())
        return result, report

    def test_runner_zero_exit_without_evidence_fails(self):
        result, report = self.run_stub(stamp=False)
        self.assertNotEqual(result, 0)
        self.assertEqual(report['status'], 'failed')

    def test_runner_failure_cannot_reuse_stale_success(self):
        self.assertEqual(self.run_stub()[0], 0)
        result, report = self.run_stub(status=7, stamp=False)
        self.assertEqual(result, 7)
        self.assertEqual(report['status'], 'failed')
        self.assertFalse(list((self.root / self.rows[0]['id']).glob('**/result.txt')))

    def test_runner_cache_miss_fails_even_with_pass_stamp(self):
        result, report = self.run_stub(proof={**PROOF, 'second_origin_package_gets': 2})
        self.assertNotEqual(result, 0)
        self.assertEqual(report['status'], 'failed')

    def test_runner_success_has_source_image_and_cache_proof(self):
        result, report = self.run_stub()
        self.assertEqual(result, 0)
        self.assertEqual(report['source_sha'], SHA)
        self.assertEqual(report['image'], self.rows[0]['image'])
        self.assertEqual(report['cache_hit'], PROOF)

    def test_heavy_matrix_has_only_manual_and_reusable_triggers(self):
        workflow = (matrix.ROOT / '.github/workflows/client-compatibility.yml').read_text()
        triggers = workflow.split('permissions:', 1)[0]
        self.assertIn('workflow_dispatch:', triggers)
        self.assertIn('workflow_call:', triggers)
        for event in ('pull_request:', 'push:', 'release:', 'schedule:'):
            self.assertNotIn(event, triggers)
        self.assertFalse((matrix.ROOT / '.github/workflows/apt-client.yml').exists())
        self.assertFalse((matrix.ROOT / '.github/workflows/dnf-client.yml').exists())
        release = (matrix.ROOT / '.github/workflows/release.yaml').read_text()
        self.assertIn('uses: ./.github/workflows/client-compatibility.yml', release)
        self.assertIn('needs: client_compatibility', release)
        self.assertIn("needs.client_compatibility.result == 'success'", release)
        self.assertIn("if: success() && startsWith(github.ref, 'refs/tags/')", release)


if __name__ == '__main__':
    unittest.main()
