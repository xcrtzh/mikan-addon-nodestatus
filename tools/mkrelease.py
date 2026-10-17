#!/usr/bin/env python3
"""Build mikan-addon.json for a release and sign it.

Env:
  RELEASE_TAG   git tag, e.g. v1.0.0
  IMAGE_DIGEST  sha256:... of the published multi-arch image
  REPO_OWNER    GitHub owner, lowercased (e.g. lofakin)
  REPO_NAME     repository name (e.g. mikan-addon-nodestatus)
  ADDON_PUBKEY  base64 Ed25519 public key for the "key" field
  MIKAN_ADDON_SIGNING_KEY  base64 Ed25519 private key seed (32 bytes)

Writes mikan-addon.json and mikan-addon.json.sig into the current directory.
"""
import base64
import json
import os
import re
import sys

from nacl.signing import SigningKey


def main() -> None:
    tag = os.environ["RELEASE_TAG"]
    version = tag[1:] if tag.startswith("v") else tag
    if not re.fullmatch(r"[0-9A-Za-z][0-9A-Za-z._+-]{0,63}", version):
        sys.exit(f"bad version from tag: {tag}")
    digest = os.environ["IMAGE_DIGEST"]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        sys.exit(f"bad digest: {digest}")
    owner = os.environ["REPO_OWNER"].lower()
    repo = os.environ["REPO_NAME"]

    with open("adapter.json", encoding="utf-8") as f:
        entry = json.load(f)
    entry["version"] = version
    entry["image"] = f"ghcr.io/{owner}/tool-nodestatus"
    entry["digest"] = digest
    entry["homepage"] = f"https://github.com/{owner}/{repo}"

    doc = {
        "key": os.environ["ADDON_PUBKEY"].strip(),
        "adapters": [entry],
    }
    data = json.dumps(doc, ensure_ascii=False, indent=2).encode("utf-8") + b"\n"
    with open("mikan-addon.json", "wb") as f:
        f.write(data)

    seed = base64.b64decode(os.environ["MIKAN_ADDON_SIGNING_KEY"].strip())
    sig = SigningKey(seed).sign(data).signature
    with open("mikan-addon.json.sig", "w", encoding="utf-8") as f:
        f.write(base64.b64encode(sig).decode() + "\n")
    print(f"mikan-addon.json: {entry['id']} {version} {digest[:19]}...")


if __name__ == "__main__":
    main()
