"""Config-only packaging contract tests; no OPS, builds, or cloud access."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name('nanos-config.py')
# Public Amazon Root CA 3 certificate, used only as a parseable PEM fixture.
PUBLIC_CA = '''-----BEGIN CERTIFICATE-----
MIIBtjCCAVugAwIBAgITBmyf1XSXNmY/Owua2eiedgPySjAKBggqhkjOPQQDAjA5
MQswCQYDVQQGEwJVUzEPMA0GA1UEChMGQW1hem9uMRkwFwYDVQQDExBBbWF6b24g
Um9vdCBDQSAzMB4XDTE1MDUyNjAwMDAwMFoXDTQwMDUyNjAwMDAwMFowOTELMAkG
A1UEBhMCVVMxDzANBgNVBAoTBkFtYXpvbjEZMBcGA1UEAxMQQW1hem9uIFJvb3Qg
Q0EgMzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABCmXp8ZBf8ANm+gBG1bG8lKl
ui2yEujSLtf6ycXYqm0fc4E7O5hrOXwzpcVOho6AF2hiRVd9RFgdszflZwjrZt6j
QjBAMA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgGGMB0GA1UdDgQWBBSr
ttvXBp43rDCGB5Fwx5zEGbF4wDAKBggqhkjOPQQDAgNJADBGAiEA4IWSoxe3jfkr
BqWTrBqYaGFy+uGh0PsceGCmQ5nFuMQCIQCcAu/xlJyzlvnrxir4tiz+OpAUFteM
YyRIHN8wfdVoOw==
-----END CERTIFICATE-----
'''


class NanosConfigTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='nanos-config-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.ca_dir = self.root / 'public-ca'
        self.ca_dir.mkdir()
        self.ca_file = self.ca_dir / 'ca-certificates.crt'
        self.ca_file.write_text(PUBLIC_CA, encoding='ascii')

    def run_config(self, mode='gcp', image=None):
        return subprocess.run(
            [sys.executable, str(SCRIPT), '--mode', mode,
             '--ca-dir', str(self.ca_dir), '--image', str(image or self.root / 'disk.raw')],
            capture_output=True, text=True, timeout=10, check=False)

    def assert_rejected(self):
        result = self.run_config()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, '')
        return result

    def test_exact_public_file_mapping_in_both_modes(self):
        for mode in ('local', 'gcp'):
            with self.subTest(mode=mode):
                result = self.run_config(mode)
                self.assertEqual(result.returncode, 0, result.stderr)
                config = json.loads(result.stdout)
                # OPS 0.1.47 splits the source into parent + basename pattern.
                # A directory source only matches the directory itself, so the
                # precise file source is the relevant external-format contract.
                self.assertEqual(config['MapDirs'], {str(self.ca_file.resolve()): '/etc/ssl/certs'})
                self.assertNotIn(str(self.ca_dir.resolve()), config['MapDirs'])
                self.assertEqual(config['Env']['SSL_CERT_FILE'], '/etc/ssl/certs/ca-certificates.crt')
                self.assertEqual(config['Env']['SMS_ENABLED'], 'false')
                self.assertEqual(config['Env']['CONFIG_FROM_METADATA'], 'true' if mode == 'gcp' else 'false')
                self.assertFalse((self.root / 'disk.raw').exists())

    def test_rejects_extra_files_without_emitting_contents(self):
        marker = 'PRIVATE-FIXTURE-MUST-NOT-APPEAR'
        (self.ca_dir / 'credentials.json').write_text(marker)
        result = self.assert_rejected()
        self.assertIn('must contain only ca-certificates.crt', result.stderr)
        self.assertNotIn(marker, result.stdout + result.stderr)

    def test_rejects_missing_bundle(self):
        self.ca_file.unlink()
        self.assert_rejected()

    def test_rejects_bundle_symlink(self):
        outside = self.root / 'outside.crt'
        outside.write_text(PUBLIC_CA)
        self.ca_file.unlink()
        self.ca_file.symlink_to(outside)
        self.assertIn('non-symlink', self.assert_rejected().stderr)

    def test_rejects_directory_instead_of_bundle(self):
        self.ca_file.unlink()
        self.ca_file.mkdir()
        self.assertIn('regular', self.assert_rejected().stderr)

    def test_rejects_private_key_marker_without_emitting_material(self):
        self.ca_file.write_text(PUBLIC_CA + '\n-----BEGIN PRIVATE KEY-----\nPRIVATE-FIXTURE\n-----END PRIVATE KEY-----\n')
        result = self.assert_rejected()
        self.assertIn('never private keys', result.stderr)
        self.assertNotIn('PRIVATE-FIXTURE', result.stdout + result.stderr)

    def test_rejects_non_certificate_and_malformed_pem(self):
        for body in ('plain text', '', '-----BEGIN CERTIFICATE-----\nnot-base64\n-----END CERTIFICATE-----\n'):
            with self.subTest(body=body):
                self.ca_file.write_text(body)
                self.assert_rejected()

    def test_rejects_non_utf8(self):
        self.ca_file.write_bytes(b'\xff\xfe')
        self.assertIn('readable UTF-8', self.assert_rejected().stderr)

    def test_rejects_relative_image(self):
        result = self.run_config(image=Path('relative.raw'))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, '')
        self.assertIn('absolute', result.stderr)


if __name__ == '__main__':
    unittest.main()
