#!/usr/bin/env python3
"""Emit a bounded, nonsecret OPS config; never build or touch cloud resources."""
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--mode', choices=('local', 'gcp'), default='gcp')
parser.add_argument('--ca-dir', type=Path, required=True,
                    help='Directory containing only public ca-certificates.crt')
parser.add_argument('--image', type=Path, required=True,
                    help='Absolute task-owned raw image output path on BuildOffload')
args = parser.parse_args()
ca_dir = args.ca_dir.resolve()
if not ca_dir.is_dir() or sorted(p.name for p in ca_dir.iterdir()) != ['ca-certificates.crt']:
    parser.error('CA directory must contain only ca-certificates.crt; do not map a credential directory')
ca_file = ca_dir / 'ca-certificates.crt'
if ca_file.is_symlink() or not ca_file.is_file():
    parser.error('CA bundle must be a regular, non-symlink file')
certs = ca_file.read_text()
if 'PRIVATE KEY' in certs or '-----BEGIN CERTIFICATE-----' not in certs:
    parser.error('CA bundle must contain public certificates, never private keys')
if not args.image.is_absolute():
    parser.error('--image must be an absolute task-owned output path')
env = {'LISTEN_ADDR': ':8080', 'SMS_ENABLED': 'false',
       'GOMAXPROCS': '2', 'GOMEMLIMIT': '768MiB',
       'SSL_CERT_FILE': '/etc/ssl/certs/ca-certificates.crt',
       'CONFIG_FROM_METADATA': 'true' if args.mode == 'gcp' else 'false'}
if args.mode == 'local':
    env.update({'PUBLIC_URL': 'http://localhost:8080', 'DATA_DIR': '/data'})
else:
    env['HTTPS_ENABLED'] = 'true'
config = {'RunConfig': {'Memory': '1G', 'Ports': ['80', '443'] if args.mode == 'gcp' else ['8080'], 'ImageName': str(args.image)},
          'MapDirs': {str(ca_dir): '/etc/ssl/certs'}, 'Env': env}
if args.mode == 'gcp':
    config['CloudConfig'] = {'ImageName': 'is-optimized'}
print(json.dumps(config, indent=2))
