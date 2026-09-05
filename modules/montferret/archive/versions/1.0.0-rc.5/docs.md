# ARCHIVE

The `archive` contrib module inspects, reads, and extracts ZIP, TAR, TAR.GZ, and
TGZ archives through Ferret's sandboxed filesystem.

```go
engine, err := ferret.New(
    ferret.WithFSRoot("/sandbox"),
    ferret.WithModules(archive.New()),
)
```

Every source read, metadata lookup, directory creation, destination write, and
failure cleanup is performed through the filesystem attached to the execution
context. URLs are not fetched: a source string is always a sandbox-visible
filesystem path. The host may deny any operation through its filesystem policy.

## List

```fql
RETURN ARCHIVE::LIST("release.zip")
```

`LIST(source, options?)` accepts `format: "auto" | "zip" | "tar" | "tar.gz" |
"tgz"`. Auto detection checks the source suffix and then a bounded content
probe. Results include the entry name, sizes, permission mode, modification
time, type flags, link target when available, and normalized format.

```fql
{
    name: "docs/readme.md",
    size: 1842,
    compressedSize: 731,
    mode: "0644",
    modTime: "2026-06-09T12:30:00Z",
    isDir: false,
    isRegular: true,
    isSymlink: false,
    linkName: NONE,
    format: "zip"
}
```

`compressedSize` is `NONE` for TAR and TAR.GZ. `modTime` is UTC RFC3339 when
present and `NONE` otherwise. TAR hardlinks use `linkName` and report both
`isRegular` and `isSymlink` as false.

## Read

```fql
LET manifest = ARCHIVE::READ("package.zip", "manifest.json", {
    as: "string"
})
RETURN JSON::PARSE(manifest)
```

`READ(source, name, options?)` reads the first matching regular entry.

- `as` is `binary` (default) or `string`.
- `missing` is `error` (default) or `none`.
- directories, links, and special entries cannot be read as files.

## Extract

```fql
RETURN ARCHIVE::EXTRACT("site.tar.gz", "dist", {
    overwrite: false,
    createDirs: true,
    include: ["public", "public/*"],
    exclude: ["public/*.map"],
    links: "skip"
})
```

Extraction validates the complete archive before creating output. Absolute,
drive-qualified, UNC, backslash-containing, empty-segment, dot-segment, and
parent-segment paths are rejected. Destination links are rejected through
Ferret's link-aware filesystem metadata capability.

Options and defaults:

```fql
{
    format: "auto",
    overwrite: false,
    createDirs: true,
    include: [],
    exclude: [],
    links: "skip"
}
```

Include and exclude patterns use Go `path.Match` semantics. Matching is
case-sensitive, `*` does not cross `/`, exclude wins, and `**` is not a
recursive wildcard. Filtered entries are omitted from results.

```fql
{
    name: "public/index.html",
    path: "dist/public/index.html",
    size: 4210,
    isDir: false,
    skipped: false,
    reason: NONE
}
```

Symbolic and hard links are skipped by default and can instead be rejected with
`links: "error"`. They are never followed or preserved. Devices, FIFOs,
sockets, and unknown entry types are rejected.

## Resource limits

`WithMaxEntrySize` limits each materialized read and extracted regular entry.
`WithMaxZIPBufferSize` limits ZIP fallback buffering when the sandbox source is
neither random-access nor seekable. Both default to 64 MiB.

Extraction streams regular files and removes incomplete outputs after read,
write, close, or cancellation failures. The current filesystem API does not
provide atomic replacement, so `overwrite: true` cannot restore an original
file after replacement begins and a later write fails.

## Not supported

The module does not create or update archives, fetch URLs, preserve links,
restore ownership, unpack device files, create unmanaged temporary files,
support encrypted/password-protected ZIP files, or support RAR, 7z, XZ, or BZ2.
