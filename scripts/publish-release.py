#!/usr/bin/env python3
"""Publish a prepared Companion release from the owner's account."""

import argparse
import base64
import hashlib
import json
import subprocess
import tempfile
from pathlib import Path
from urllib.request import Request, urlopen

REPO = "Yaneart/yaneCompanion"
ROOT = Path(__file__).resolve().parents[1]
FEED = f"repos/{REPO}/contents/release.json"
BRANCH = "updates"


def check(condition, message):
    if not condition:
        raise ValueError(message)


def github(path, *, body=None, missing=False):
    command = ["gh", "api", path]
    if body is not None:
        command += ["--method", "PUT", "--input", "-"]
    result = subprocess.run(command, input=json.dumps(body) if body is not None else None,
                            capture_output=True, text=True)
    if missing and result.returncode and "(HTTP 404)" in result.stderr:
        return None
    check(result.returncode == 0, result.stderr.strip())
    return json.loads(result.stdout)


def read_manifest(envelope, public_key, *, historical=False):
    go = next((p for p in (ROOT / ".tools/go/bin/go", ROOT / ".tools/go/bin/go.exe")
               if p.is_file()), "go")
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "release.json"
        path.write_bytes(envelope)
        command = [str(go), "run", "./cmd/release-manifest", "-manifest", str(path),
                   "-public-key", public_key]
        if historical:
            command.append("-historical")
        subprocess.run(command, cwd=ROOT, check=True, stdout=subprocess.DEVNULL)
    return json.loads(base64.b64decode(json.loads(envelope)["payload"]))


def fingerprint(path):
    with path.open("rb") as stream:
        return path.stat().st_size, hashlib.file_digest(stream, "sha256").hexdigest()


def prepare(directory, manifest):
    current = manifest["current"]
    version = current["version"]
    base = f"https://github.com/{REPO}/releases/download/{version}"
    packages = current["packages"]
    check(len(packages) == 4, "The signed manifest must include all four installers.")
    expected = {p["name"]: (p["size"], p["sha256"]) for p in packages}
    for package in packages:
        check(package["url"] == f"{base}/{package['name']}", "Manifest has an unexpected download URL.")
    index = json.loads((directory / "packages.json").read_bytes())
    indexed = {p["name"]: (p["size"], p["sha256"]) for p in index["artifacts"]}
    check(index["version"] == version and len(index["artifacts"]) == 4 and indexed == expected,
          "packages.json does not match the signed manifest.")
    for name, digest in expected.items():
        check(fingerprint(directory / name) == digest, f"Local file differs from manifest: {name}")
    expected["packages.json"] = fingerprint(directory / "packages.json")
    return expected, index["signedWindowsRelease"]


def check_transition(prior, manifest):
    check(manifest["sequence"] > prior["sequence"], "The feed already has this or a newer sequence.")
    current, published = manifest["current"], prior["current"]
    if current["version"] == published["version"]:
        check(current == published and manifest.get("previous") == prior.get("previous"),
              "Renewal must preserve published packages and rollback metadata.")
    else:
        check(manifest.get("previous") == published,
              "A new release must keep the current published release as its rollback version.")


def get_release(version):
    release = github(f"repos/{REPO}/releases/tags/{version}", missing=True)
    if release is not None:
        return release
    # The tag endpoint may exclude drafts. Keep incomplete uploads resumable.
    for page in range(1, 1000):
        releases = github(f"repos/{REPO}/releases?per_page=100&page={page}")
        for release in releases:
            if release["tag_name"] == version:
                return release
        if len(releases) < 100:
            return None
    raise ValueError("Unable to locate the release in the repository's release list.")


def missing_assets(release, expected):
    missing = set(expected)
    for asset in release["assets"]:
        name = asset["name"]
        check(name in missing and asset["state"] == "uploaded" and
              (asset["size"], asset["digest"]) == (expected[name][0], f"sha256:{expected[name][1]}"),
              f"GitHub has an unexpected or incomplete asset: {name}")
        missing.remove(name)
    return sorted(missing)


def release_command(action, *args):
    subprocess.run(["gh", "release", action, *map(str, args), "--repo", REPO], check=True)


def publish(args, envelope, manifest, expected, signed_windows, public_key):
    version = manifest["current"]["version"]
    github(f"repos/{REPO}/branches/{BRANCH}")
    feed = github(f"{FEED}?ref={BRANCH}", missing=True)
    published = base64.b64decode(feed["content"]) if feed else b""
    if feed and published != envelope:
        check_transition(read_manifest(published, public_key, historical=True), manifest)
    elif not feed:
        check(manifest["sequence"] == 1 and "previous" not in manifest,
              "The first channel publication must start with sequence 1 and no rollback version.")

    release = get_release(version)
    if release is None or release["draft"]:
        pending = subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True)
        check(not pending, "The source checkout must be clean before publishing a new tag.")
        commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
        github(f"repos/{REPO}/commits/{commit}")
        ref = github(f"repos/{REPO}/git/ref/tags/{version}", missing=True)
        tag = github(f"repos/{REPO}/commits/{version}") if ref else None
        check(tag is None or tag["sha"] == commit, "The release tag already points to a different commit.")
        if release is not None and tag is None:
            check(release["target_commitish"] == commit, "The existing draft targets a different commit.")
    if release is None:
        notes = args.notes_file.read_text(encoding="utf-8") if args.notes_file else f"yaneCompanion {version}."
        if not signed_windows:
            notes += "\n\nУстановщик Windows без подписи Authenticode."
        release_command("create", version, "--draft", "--target", commit,
                        "--title", f"yaneCompanion {version}", "--notes", notes)
        release = get_release(version)
    check(release is not None and not release["prerelease"], "Expected a regular release or draft.")
    missing = missing_assets(release, expected)
    if release["draft"]:
        if missing:
            release_command("upload", version, *(args.packages / name for name in missing))
        check(not missing_assets(get_release(version), expected), "The draft is missing assets.")
        release_command("edit", version, "--draft=false", "--latest")
        release = get_release(version)
    check(release is not None and not release["draft"] and not missing_assets(release, expected),
          "The release must be public and have matching assets before updating the channel.")
    for asset in release["assets"]:
        url = f"https://github.com/{REPO}/releases/download/{version}/{asset['name']}"
        check(asset["browser_download_url"] == url, f"Unexpected download URL: {asset['name']}")
        with urlopen(Request(url, headers={"Range": "bytes=0-0"}), timeout=20) as response:
            check(response.status in (200, 206) and response.read(1), f"Asset is unavailable: {asset['name']}")

    if published != envelope:
        body = {"branch": BRANCH, "message": f"Publish Companion update manifest for {version}",
                "content": base64.b64encode(envelope).decode("ascii")}
        if feed:
            body["sha"] = feed["sha"]
        github(FEED, body=body)
    uploaded = github(f"{FEED}?ref={BRANCH}")
    check(base64.b64decode(uploaded["content"]) == envelope, "GitHub returned a different manifest.")
    print(release["html_url"])
    print(f"https://raw.githubusercontent.com/{REPO}/{BRANCH}/release.json")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--packages", type=Path, default=ROOT / "dist/packages")
    parser.add_argument("--manifest", type=Path, help="default: release.json in the package directory")
    parser.add_argument("--public-key-file", type=Path, default=Path.home() / ".local/share/yanecompanion-release/ed25519.pub")
    parser.add_argument("--notes-file", type=Path, help="release notes for a new tag")
    parser.add_argument("--publish", action="store_true", help="publish the release and update the updates branch")
    args = parser.parse_args()
    args.packages = args.packages.resolve()
    envelope = (args.manifest or args.packages / "release.json").read_bytes()
    public_key = args.public_key_file.read_text().strip()
    manifest = read_manifest(envelope, public_key)
    expected, signed_windows = prepare(args.packages, manifest)
    print(f"Verified yaneCompanion {manifest['current']['version']}, sequence {manifest['sequence']}, {len(expected)} files.", flush=True)
    if args.publish:
        publish(args, envelope, manifest, expected, signed_windows, public_key)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"Publication stopped: {error}") from None
