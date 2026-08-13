"""Build the Bounty plugin marketplace.

For each plugin dir under plugins/, create a zip (deterministic) and write
registry.json with sha256 pins. Run from repo root:

    python marketplace/build.py
"""
import hashlib
import json
import pathlib
import zipfile

ROOT = pathlib.Path(__file__).parent
PLUGINS = ROOT / "plugins"
RAW_BASE = "https://raw.githubusercontent.com/BJC666/Bounty1.0/main/marketplace/plugins"
OWNER = "BJC666"


def zip_plugin(src: pathlib.Path) -> pathlib.Path:
    out = PLUGINS / (src.name + ".zip")
    files = sorted(p for p in src.rglob("*") if p.is_file())
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as zf:
        for f in files:
            zf.write(f, f.relative_to(src).as_posix())
    return out


def main():
    entries = []
    for src in sorted(PLUGINS.iterdir()):
        if not src.is_dir():
            continue
        toml = (src / "plugin.toml").read_text(encoding="utf-8")
        vals = {}
        for line in toml.splitlines():
            if "=" in line:
                k, v = line.split("=", 1)
                vals[k.strip()] = v.strip().strip(chr(34))
        zpath = zip_plugin(src)
        digest = hashlib.sha256(zpath.read_bytes()).hexdigest()
        entries.append({
            "name": vals.get("name", src.name),
            "version": vals.get("version", "0.0.0"),
            "description": vals.get("description", ""),
            "author": OWNER,
            "download": RAW_BASE + "/" + zpath.name,
            "sha256": digest,
        })
    (ROOT / "registry.json").write_text(
        json.dumps(entries, ensure_ascii=False, indent=2) + chr(10), encoding="utf-8")
    print("registry.json:", len(entries), "plugins")


if __name__ == "__main__":
    main()