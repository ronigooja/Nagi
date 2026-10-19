#!/usr/bin/env python3
"""Refresh checked-in native descriptor resources from named Go catalogs.

Maintainer command only; building/running Nagi never executes this script.
Usage: python3 macos/NagiApp/Tools/update-message-catalog.py [CATALOG_DIRECTORY]
All producer modules and menu.runner descriptors are consumed. Local interactive
menu resources remain frontend-owned. Human-only
Go printf templates are excluded until migrated to the named descriptor contract.
"""
import json
import pathlib
import re
import sys

app = pathlib.Path(__file__).resolve().parents[1]
catalogs = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else app.parents[1] / 'internal/i18n/catalogs'
resources = app / 'Sources/NagiApp/Resources'
signatures = {
    'cli.language.invalid': {'value': 'string'},
    'cli.language.missing': {},
    'cli.language.duplicate': {},
}
texts = {
    'en': {
        'cli.language.invalid': 'Unsupported language {value}; use en or zh-Hans.',
        'cli.language.missing': '--lang requires a language; use en or zh-Hans.',
        'cli.language.duplicate': '--lang may be specified only once.',
    },
    'zh-Hans': {
        'cli.language.invalid': '不支持语言 {value}；请使用 en 或 zh-Hans。',
        'cli.language.missing': '--lang 需要语言参数；请使用 en 或 zh-Hans。',
        'cli.language.duplicate': '--lang 只能指定一次。',
    },
}
excluded = []
for schema_file in sorted(catalogs.glob('*.arguments.json')):
    module = schema_file.name.removesuffix('.arguments.json')
    schema = json.loads(schema_file.read_text())
    translations = {lang: json.loads((catalogs / lang / (module + '.json')).read_text()) for lang in texts}
    for message_id, signature in schema.items():
        if module == 'menu' and not message_id.startswith('menu.runner.'):
            continue
        if any(kind not in ('string', 'number', 'boolean') for kind in signature.values()):
            raise ValueError(f'{message_id}: unsupported scalar signature')
        values = {lang: entries[message_id] for lang, entries in translations.items()}
        # These are legacy human-only formatting calls, never named descriptors.
        if any(re.search(r'%(?:\[\d+\])?[-+ #0-9.]*[vsdqtxXfgEeGbcTUOop]', value) for value in values.values()):
            if not message_id.startswith('cli.output.'):
                raise ValueError(f'{message_id}: descriptor uses Go printf instead of named arguments')
            excluded.append(message_id)
            continue
        for lang, value in values.items():
            actual = set(re.findall(r'\{([A-Za-z_][A-Za-z_0-9]*)\}', value))
            if actual != set(signature):
                raise ValueError(f'{message_id}/{lang}: placeholders {actual} != {set(signature)}')
        if message_id in signatures and signatures[message_id] != signature:
            raise ValueError(f'{message_id}: conflicting module signatures')
        signatures[message_id] = signature
        for lang, value in values.items():
            texts[lang][message_id] = value
fixture_path = app.parents[1] / 'docs/design/localization-contract.json'
fixture = json.loads(fixture_path.read_text())
fixture['producer_ids'] = dict(sorted(signatures.items()))
fixture_path.write_text(json.dumps(fixture, ensure_ascii=False, indent=2) + '\n')
resources.joinpath('MessageSignatures.json').write_text(json.dumps(signatures, ensure_ascii=False, indent=2, sort_keys=True) + '\n')
for lang, entries in texts.items():
    lines = ['/* Named CLI/backend descriptors. Refresh with Tools/update-message-catalog.py. */']
    for message_id, value in sorted(entries.items()):
        names = sorted(signatures[message_id])
        value = value.replace('%', '%%')
        value = re.sub(r'\{([A-Za-z_][A-Za-z_0-9]*)\}', lambda match: '%' + str(names.index(match[1]) + 1) + '$@', value)
        lines.append(json.dumps(message_id, ensure_ascii=False) + ' = ' + json.dumps(value, ensure_ascii=False) + ';')
    resources.joinpath(lang + '.lproj', 'MessageCatalog.strings').write_text('\n'.join(lines) + '\n')
print(f'Generated {len(signatures)} named descriptors; excluded {len(excluded)} legacy human-only printf templates.')
