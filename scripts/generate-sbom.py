#!/usr/bin/env python3
import argparse
import hashlib
import json
import pathlib
import uuid

parser = argparse.ArgumentParser()
parser.add_argument("--output", required=True)
parser.add_argument("--version", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parent.parent
files = []
for path in sorted(root.rglob("*")):
    if not path.is_file():
        continue
    relative = path.relative_to(root).as_posix()
    if relative.startswith((".git/", "dist/", "bin/", "build/")) or relative == "coverage.out":
        continue
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    files.append({
        "SPDXID": "SPDXRef-File-" + hashlib.sha256(relative.encode()).hexdigest()[:16],
        "fileName": "./" + relative,
        "checksums": [{"algorithm": "SHA256", "checksumValue": digest}],
        "licenseConcluded": "Apache-2.0",
        "licenseInfoInFiles": ["Apache-2.0"],
        "copyrightText": "Copyright 2026 WorldBisect contributors",
    })
namespace = f"https://github.com/ClusterPilot-System/worldbisect/releases/tag/v{args.version}/spdx-{uuid.uuid5(uuid.NAMESPACE_URL, args.version)}"
document = {
    "spdxVersion": "SPDX-2.3",
    "dataLicense": "CC0-1.0",
    "SPDXID": "SPDXRef-DOCUMENT",
    "name": f"worldbisect-{args.version}",
    "documentNamespace": namespace,
    "creationInfo": {
        "created": "1970-01-01T00:00:00Z",
        "creators": ["Tool: worldbisect/scripts/generate-sbom.py"],
        "licenseListVersion": "3.25",
    },
    "packages": [{
        "name": "worldbisect",
        "SPDXID": "SPDXRef-Package-worldbisect",
        "versionInfo": args.version,
        "downloadLocation": "https://github.com/ClusterPilot-System/worldbisect",
        "filesAnalyzed": True,
        "licenseConcluded": "Apache-2.0",
        "licenseDeclared": "Apache-2.0",
        "copyrightText": "Copyright 2026 WorldBisect contributors",
        "packageVerificationCode": {
            "packageVerificationCodeValue": hashlib.sha1("".join(item["checksums"][0]["checksumValue"] for item in files).encode()).hexdigest()
        },
    }],
    "files": files,
    "relationships": [
        {"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": "SPDXRef-Package-worldbisect"},
        *[{"spdxElementId": "SPDXRef-Package-worldbisect", "relationshipType": "CONTAINS", "relatedSpdxElement": item["SPDXID"]} for item in files],
    ],
}
path = pathlib.Path(args.output)
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")
