# The Unicode data the tokenizer's grapheme tables are generated from

`17.0.0/` holds four files of the Unicode Character Database, version 17.0.0, exactly as published at
`https://www.unicode.org/Public/17.0.0/ucd/` (fetched 2026-10-07):

| File | What is read from it |
|---|---|
| `auxiliary/GraphemeBreakProperty.txt` | each character's Grapheme_Cluster_Break class |
| `emoji/emoji-data.txt` | Extended_Pictographic |
| `DerivedCoreProperties.txt` | Indic_Conjunct_Break (InCB) |
| `auxiliary/GraphemeBreakTest.txt` | the standard's conformance test, copied to `../../testdata/` and run as a Go test |

They are the Unicode Consortium's, © Unicode, Inc., under the Unicode License v3, a copy of which is
`LICENSE-UNICODE` here; each file carries its own notice.

**Why 17.0.0 and no other.** The version is the one the reference tokenizer segments by. It was found by
asking the reference, not assumed: it joins a combining mark first assigned in Unicode 17.0 (U+1AD3) to the
character before it, and does not join the neighbouring code point that 17.0 leaves unassigned (U+1ADE).
A table of another version disagrees with the reference about every mark assigned between the two.

To regenerate: `python3 gen.py 17.0.0 ../../grapheme_tables.go`. The generator refuses files that do not
all name one version.
