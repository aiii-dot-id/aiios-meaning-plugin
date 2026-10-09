#!/usr/bin/env python3
"""Write plugin.json's "runtimes" from what the kit's packer printed for each platform's runtime archive.

    scripts/runtimes.py <url of the directory the archives are published in> <report.json> <report.json> <report.json>

A report is the JSON `aiisdk runtime-pack -dir <tree> -o meaning-runtime-<variant_id>.tar.gz` prints. Each
archive is named for its variant, and that name is how a report finds its variant here. Every figure
a host holds a runtime to is the packer's for that archive; none is typed by hand. A tree past the host's
default ceilings is refused: this runtime is a worker and a library, and one that is not has gone wrong.

The tree packed for a variant holds, at its top and with no directory inside it: the worker (meaning-worker,
or meaning-worker.exe), the ONNX Runtime library it was linked with where that is a file of its own, on
Windows the C++ runtime's libraries those two load, and the licence and notice files of what the tree
holds. No model data: the tokenizer file is a declared model. The carrier is not in it: the host places
the signed package's carrier at the root of the installed tree.
"""
import json
import os
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PLUGIN = os.path.join(ROOT, "plugin", "native", "cmd", "aii-meaning-t3", "plugin.json")
FIGURES = ("sha256", "size", "installed_bytes", "files", "inventory_sha256", "largest_file_bytes", "depth")


def main(base, reports):
    with open(PLUGIN, encoding="utf-8") as f:
        cfg = json.load(f)
    by_variant = {}
    for path in reports:
        with open(path, encoding="utf-8") as f:
            rep = json.load(f)
        if not all(k in rep for k in ("archive",) + FIGURES):
            raise SystemExit("%s is not a report of aiisdk runtime-pack" % path)
        if "requires_operator_ceilings" in rep:
            raise SystemExit("%s: the archive is past the host's default ceilings: %s" % (path, rep["requires_operator_ceilings"]))
        name = os.path.basename(rep["archive"])
        variant = name.removeprefix("meaning-runtime-").removesuffix(".tar.gz")
        if name == variant or variant in by_variant:
            raise SystemExit("%s: archive %s is not meaning-runtime-<variant_id>.tar.gz, or is named twice" % (path, name))
        by_variant[variant] = {"variant_id": variant, "url": base.rstrip("/") + "/" + name, **{k: rep[k] for k in FIGURES}}
    declared = [v["variant_id"] for v in cfg["variants"]]
    if sorted(by_variant) != sorted(declared):
        raise SystemExit("reports for %s; plugin.json declares %s" % (sorted(by_variant), sorted(declared)))
    cfg["runtimes"] = [by_variant[v] for v in declared]
    with open(PLUGIN, "w", encoding="utf-8") as f:
        f.write(json.dumps(cfg, indent=2, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    if len(sys.argv) < 3 or not sys.argv[1].startswith("https://"):
        raise SystemExit(__doc__)
    main(sys.argv[1], sys.argv[2:])
