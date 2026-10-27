#!/usr/bin/env python3
"""Emit nonsecret OPS config. Never embed a credential into a boot image."""
import json, os, sys
# Runtime nonsecret configuration is retrieved from the instance attribute
# created by Pulumi. Credential values never enter this file or the image.
print(json.dumps({'RunConfig': {'Memory': '1G', 'Ports': ['8080']},
                  'Env': {'LISTEN_ADDR': ':8080', 'CONFIG_FROM_METADATA': 'true'}}, indent=2))
