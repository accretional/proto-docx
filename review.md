# review.md — notes on `docx.proto`

Context: the vendored copy of `proto/openformat/v1/docx.proto`
(4147 lines, 312 messages, 90 enums) is the primary upstream schema for
this project. Below are the issues a careful reviewer would flag — split
by category, with line numbers keyed to the current vendored file.

The `go_package` option on line 29 was modified locally from
`openformat/gen/go/openformat/v1;openformatv1` to
`openformat-docx/gen/go/openformat/v1;openformatdocxv1` so DOCX-derived
types generate into this module rather than collide with proto-xml's
mime/xml types. That change is intentional and is **not** an upstream
bug.

---

## 1. Semantic / naming bugs

1. **Typo in field name — `conform_ance_strict` (line 150).** Should be
   `conformance_strict` (no spurious underscore). Worth upstreaming a
   rename before the schema gets wire-level consumers.
2. **Enum/type mismatch — `DrawingVerticalPosition.v_align` (line 1919)
   references `VerticalAlignment`, but the relevant enum is
   `DrawingVerticalAlignment` (line 1959).** The `VerticalAlignment`
   enum at line 942 models a completely different concept (baseline /
   superscript / subscript of text). At minimum a rename; possibly a
   regenerated-Go bug we only won't notice until we actually populate
   this field.
3. **Duplicate-style / overlapping enums** — `VerticalAlignment`
   (line 942, text runs) vs. `VerticalAlignValue` (line 1396, cells /
   section) vs. `DrawingVerticalAlignment` (line 1959, drawings). Three
   vertical-alignment enums with different value sets and similar names
   invites miswiring. Fine if deliberate; worth documenting the axis
   each lives on.
4. **`Compatibility.adjustSpaceFlowingContent` (line 3268) is the lone
   camelCase field** in a file otherwise written in `snake_case`.
   Almost certainly a copy-paste slip from the OOXML spec.

## 2. Under-typed fields

5. **`Style.rsid` (line 2633) is `int32`.** Elsewhere in the schema
   (`RevisionSaveIds.rsid_r` etc., line 3877) RSIDs are correctly
   modeled as `string` — OOXML RSIDs are 8-hex-char tokens, not
   integers. This field will silently truncate for typical inputs.
6. **`AbstractNum.multi_level_type` (line 2707) is `string` with a
   docstring enumeration ("singleLevel" / "multilevel" /
   "hybridMultilevel").** Should be a `MultiLevelType` enum.
7. **`AbstractNum.tmpl` (line 2708) is `string` carrying an 8-hex-digit
   template code.** Parallel to `nsid` (line 2706). Both could be
   `bytes` (4 bytes) or a dedicated `Hex32` wrapper.
8. **`ShapeOutline.val` (line 2379) is `string`** with a docstring
   enumeration of border styles ("solid", "dot", "dash", "lgDash", …).
   `BorderStyleValue` enum would catch typos and make exhaustive
   switches possible.
9. **`PaperSource.first` / `.other` (lines 1518–1519) are bare
   `int32`** with no comment or bounds. OOXML tray indices are
   printer-specific integers; at minimum a docstring and probably
   `uint32`.
10. **`FormFieldData.entry_macro` / `exit_macro` (lines 623–624) are
    bare `string`.** They carry *macro names*, not arbitrary user
    strings — worth a docstring or a `MacroRef` wrapper so consumers
    know to sanitize before execution.

## 3. "Present vs. absent" (proto3 wrapper needs)

11. **`Comment.id` (line 1787) is `int32`.** Proto3 zero-value
    semantics make `id == 0` indistinguishable from "unset". If ID `0`
    is a valid comment (the spec doesn't forbid it), this should be
    `google.protobuf.Int32Value`.
12. **Same issue on `Style.rsid` (line 2633)** — zero is a valid RSID,
    so the absence signal is lost even before you hit the type problem
    in #5.

## 4. Missing / opaque format coverage

13. **DrawingML chart / diagram / table graphics (line 2051 oneof
    `GraphicObject.data`)** are declared but their messages are either
    unexpanded stubs or backed by `bytes` blobs
    (`TableGraphic.table_data = bytes`, line 2482). A docx that
    contains any chart or SmartArt currently round-trips as opaque
    bytes — fine for raw-bytes-faithful codecs, not for introspection.
14. **VML — `Picture.vml_data` (line 2520) is raw XML bytes.** Legacy
    VML survives in real-world docs (shapes, headers/footers in older
    templates). No schema link; parsing is caller's problem.
15. **OLE — `Object.ole_data` (line 2527) is bytes.** Embedded OLE
    objects (spreadsheets, equations authored in old Equation Editor,
    …) are opaque. Related formats worth their own protos:
    - Compound File Binary (CFBF) container
    - Embedded Excel (.xlsx fragments)
    - Equation Editor 3.0 legacy equations
16. **Theme bits — `Theme.object_defaults` and `extra_clr_scheme_lst`
    (lines 3463–3464) are bytes.** DrawingML theme data. Same story as
    #13: opaque, round-trip-safe, not inspectable.
17. **No macro / `.docm` VBA storage.** The schema models
    macro-*references* (lines 623–624) but not macro storage
    (`word/vbaProject.bin`). Should be acknowledged explicitly in the
    file header, either as out-of-scope or as a future proto.
18. **No package-level XMLDSig.** `word/_rels/_rels/document.xml.rels`
    digital signatures and the `_xmlsignatures/` folder are absent.
    Signed-document fidelity is currently achieved only via `RawBytes`.
19. **No custom-XML-schema parsing.** `CustomXmlPart` (line 81) and
    `AttachedSchema` (line 3316) coexist but the link is weak —
    `CustomXmlPart.schema_data` is `bytes`. Sufficient for
    pass-through, insufficient for introspection of vertical use cases
    (regulatory filings, eDiscovery).

## 5. Related media / encoding formats to consider

For `proto-docx/integration-tasks.md`-style follow-on work, these are
the XML or XML-adjacent formats directly implied by `docx.proto` that
*don't* have their own schema yet:

| Format | Where it surfaces in `docx.proto` | Would live in |
| --- | --- | --- |
| DrawingML (charts, diagrams, shapes, pictures) | `GraphicObject.data` oneof, lines 2050–2055 | new `proto-drawingml` |
| OMML (Office Math) | not modeled; referenced indirectly via `w:oMath` in text runs | new `proto-omml` |
| VML (legacy graphics) | `Picture.vml_data` bytes, line 2520 | new `proto-vml` |
| OLE CFBF containers | `Object.ole_data` bytes, line 2527 | new `proto-cfbf` |
| XMLDSig / XAdES signatures | not modeled (gap) | new `proto-xmldsig` |
| XPS (MS's PDF rival) | sibling format; same OPC container family | new `proto-xps` |
| OPC / OCF generic container | underlies `.docx`, `.xlsx`, `.pptx`, `.epub` | new `proto-opc` (biggest unlock) |

The OPC container is the highest-leverage missing piece — factoring it
out would let us share ZIP-of-XML scaffolding across DOCX, XLSX, PPTX,
and EPUB instead of each codec rolling its own `archive/zip` loop.

## 6. Imports

20. **Unused import — `import "openformat/v1/mime.proto"` (line 35).**
    No `MimeType` appears in the file body. protoc-gen-go emits a
    warning on every build. Candidates to fix upstream:
    - Remove the import, or
    - Use `MimeType` on `MediaPart` and `CustomXmlPart` in place of
      the bare `string content_type` field they currently carry.

## 7. Modeling: structure vs. content

21. **The top-level `Package` message (lines 43–95) flattens the OPC
    hierarchy.** It mixes structural parts (document, styles,
    numbering, settings) with content parts (comments, footnotes,
    endnotes, headers, footers) and parts that are actually
    *relationships* (`PackageRelationships`). Pragmatic — but the
    relationship graph between parts is lost at the proto level. A
    `PartIndex` or `PackagePartGraph` message would let consumers walk
    the package without re-reading the ZIP.
22. **Header / footer parts are duplicated** — `Package.header_parts`
    (line 88) is a repeated list, but `SectionProperties` also carries
    `HeaderReference` / `FooterReference` items pointing into those
    parts. Which is canonical? Docs should pick one.

## 8. `oneof` usage

23. **`GraphicObject.data` oneof (line 2051)** includes
    `LockedCanvasGraphic locked_canvas = 5;` but the message is a
    stub. Asymmetric with the other four arms.
24. **`FormFieldData.specific` oneof (lines 628–632)** has no
    "unspecified" arm. Fine under proto3 (unset is representable), but
    docstring should call that out.

## 9. Comments / docs

25. **The file header (lines 1–24) describes the schema's ambitions**
    ("A technically faithful representation of OOXML WordprocessingML")
    **but doesn't scope out what's explicitly not modeled** — macros,
    digital signatures, DrawingML internals, VML, OLE. A "Not modeled"
    section up top would save future reviewers time.

---

## Verdict

The schema is broadly complete for **document *content*** (paragraphs,
runs, tables, styles, numbering, settings, sections, headers, footers,
comments, notes, tracked changes). It is **deliberately loose on
embedded binary media** (charts, diagrams, VML, OLE, theme blobs) —
those arrive as `bytes` and round-trip via `RawBytes` fidelity. For
the current `proto-docx` goal (decode → typed proto → encode without
byte drift), that division is serviceable.

Priority items to flag upstream:

- Fix the `conform_ance_strict` typo (#1).
- Resolve the `VerticalAlignment` / `DrawingVerticalAlignment` type
  mismatch (#2).
- Change `Style.rsid` from `int32` to `string` (#5, #12).
- Remove or actually use the `mime.proto` import (#20).

Everything else is quality-of-life — worth filing, but the codec works
around them today via raw-bytes fidelity.
